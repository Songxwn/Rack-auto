// Package winpefix prepares official Setup boot.wim for wimboot.
// Microsoft Setup PE expects SYSTEMROOT at X:\$windows.~bt\Windows;
// wimboot expands to X:\Windows. We rewrite InstRoot/SystemRoot in the
// SOFTWARE hive (same effect as DISM /Set-TargetPath:X:\).
package winpefix

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	markerSuffix = ".targetpath-ok"
	hiveInWIM    = "Windows/System32/config/SOFTWARE"
)

// FixBootWIM rewrites WinPE TargetPath so wimboot can start Setup boot.wim.
// Requires wimlib-imagex on PATH (Debian/Ubuntu: apt install wimtools).
// Idempotent via a sidecar marker file.
func FixBootWIM(bootWIM string) error {
	bootWIM = strings.TrimSpace(bootWIM)
	if bootWIM == "" {
		return fmt.Errorf("boot.wim path empty")
	}
	st, err := os.Stat(bootWIM)
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return fmt.Errorf("boot.wim empty")
	}
	marker := bootWIM + markerSuffix
	if ms, err := os.Stat(marker); err == nil && !ms.ModTime().Before(st.ModTime()) {
		return nil
	}
	wimlib, err := lookWimlib()
	if err != nil {
		return err
	}
	count, err := imageCount(wimlib, bootWIM)
	if err != nil {
		return err
	}
	if count < 1 {
		count = 1
	}
	tmp, err := os.MkdirTemp("", "rackauto-winpe-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var patchedAny bool
	for idx := 1; idx <= count; idx++ {
		hivePath := filepath.Join(tmp, fmt.Sprintf("SOFTWARE.%d", idx))
		if err := extractHive(wimlib, bootWIM, idx, hivePath); err != nil {
			// Some indexes may not be PE; skip quietly if extract fails for non-first.
			if idx == 1 {
				return fmt.Errorf("extract SOFTWARE (index %d): %w", idx, err)
			}
			continue
		}
		raw, err := os.ReadFile(hivePath)
		if err != nil {
			return err
		}
		out, n := PatchSOFTWAREHive(raw)
		if n == 0 {
			continue
		}
		if err := os.WriteFile(hivePath, out, 0o644); err != nil {
			return err
		}
		if err := updateHive(wimlib, bootWIM, idx, hivePath); err != nil {
			return fmt.Errorf("update SOFTWARE (index %d): %w", idx, err)
		}
		patchedAny = true
	}
	if !patchedAny {
		// Already X:\ layout, or hive had no $windows.~bt — still mark OK.
		_ = os.WriteFile(marker, []byte("noop\n"), 0o644)
		return nil
	}
	return os.WriteFile(marker, []byte("patched\n"), 0o644)
}

// PatchSOFTWAREHive rewrites Setup TargetPath strings in a SOFTWARE hive.
// Shorter replacements are null-padded in-place so hive cell sizes stay valid.
// Returns the (possibly modified) buffer and how many path rewrites were applied.
func PatchSOFTWAREHive(hive []byte) ([]byte, int) {
	if len(hive) < 32 {
		return hive, 0
	}
	out := bytes.Clone(hive)
	n := 0
	// Longest first so we don't partially match.
	replacements := []struct {
		from string
		to   string
	}{
		{`X:\$windows.~bt\Windows`, `X:\Windows`},
		{`X:\$Windows.~BT\Windows`, `X:\Windows`},
		{`X:\$WINDOWS.~BT\Windows`, `X:\Windows`},
		{`X:\$windows.~bt\`, `X:\`},
		{`X:\$Windows.~BT\`, `X:\`},
		{`X:\$WINDOWS.~BT\`, `X:\`},
		{`X:\$windows.~bt`, `X:\`},
		{`X:\$Windows.~BT`, `X:\`},
		{`X:\$WINDOWS.~BT`, `X:\`},
	}
	for _, r := range replacements {
		n += replaceUTF16LEPad(out, r.from, r.to)
	}
	// Catch remaining case variants of the marker substring by scanning.
	n += replaceWindowsBTMarker(out)
	return out, n
}

func replaceWindowsBTMarker(buf []byte) int {
	// Replace leftover UTF-16LE `$windows.~bt` (any ASCII case) with `$` + null padding
	// only when it still appears as a path fragment — safer: replace full
	// `\x00$\x00w...\x00t` 13-char token with 13 nulls after ensuring we already
	// fixed the main InstRoot/SystemRoot forms above.
	const needle = `$windows.~bt`
	n := 0
	from := encodeUTF16LE(needle)
	for i := 0; i+len(from) <= len(buf); i += 2 {
		if !utf16EqualFoldASCII(buf[i:i+len(from)], from) {
			continue
		}
		// Wipe the marker in place with zeros (breaks leftover references).
		for j := range from {
			buf[i+j] = 0
		}
		n++
		i += len(from) - 2
	}
	return n
}

func replaceUTF16LEPad(buf []byte, from, to string) int {
	if from == "" || len(to) > len(from) {
		return 0
	}
	fromU := encodeUTF16LE(from)
	toU := encodeUTF16LE(to)
	n := 0
	for i := 0; i+len(fromU) <= len(buf); i += 2 {
		if !utf16EqualFoldASCII(buf[i:i+len(fromU)], fromU) {
			continue
		}
		copy(buf[i:i+len(toU)], toU)
		// Null-pad the remainder of the old string (keep cell size).
		for j := len(toU); j < len(fromU); j++ {
			buf[i+j] = 0
		}
		n++
		i += len(fromU) - 2
	}
	return n
}

func encodeUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, len(u)*2)
	for i, c := range u {
		out[i*2] = byte(c)
		out[i*2+1] = byte(c >> 8)
	}
	return out
}

func utf16EqualFoldASCII(have, want []byte) bool {
	if len(have) != len(want) || len(have)%2 != 0 {
		return false
	}
	for i := 0; i < len(have); i += 2 {
		a := uint16(have[i]) | (uint16(have[i+1]) << 8)
		b := uint16(want[i]) | (uint16(want[i+1]) << 8)
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func lookWimlib() (string, error) {
	if p, err := exec.LookPath("wimlib-imagex"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("wimlib-imagex not found; install wimtools (Debian/Ubuntu: apt install -y wimtools) so Rack-auto can fix WinPE TargetPath for wimboot")
}

func wimlibCmd(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	cmd.Env = append(os.Environ(), "WIMLIB_IMAGEX_IGNORE_CASE=1")
	return cmd
}

func imageCount(wimlib, wim string) (int, error) {
	cmd := wimlibCmd(wimlib, "info", wim)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("wimlib info: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "image count:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				n, err := strconv.Atoi(fields[len(fields)-1])
				if err == nil && n > 0 {
					return n, nil
				}
			}
		}
	}
	return 1, nil
}

func extractHive(wimlib, wim string, index int, destFile string) error {
	dir := filepath.Dir(destFile)
	cmd := wimlibCmd(wimlib, "extract", wim, strconv.Itoa(index),
		hiveInWIM, "--dest-dir="+dir, "--no-acls")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	candidates := []string{
		filepath.Join(dir, "Windows", "System32", "config", "SOFTWARE"),
		filepath.Join(dir, "SOFTWARE"),
		filepath.Join(dir, "windows", "system32", "config", "SOFTWARE"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.Size() > 0 {
			if c == destFile {
				return nil
			}
			raw, err := os.ReadFile(c)
			if err != nil {
				return err
			}
			return os.WriteFile(destFile, raw, 0o644)
		}
	}
	return fmt.Errorf("SOFTWARE hive not found after extract (%s)", strings.TrimSpace(string(out)))
}

func updateHive(wimlib, wim string, index int, hiveFile string) error {
	// Quote-free path: hiveFile is under our temp dir (no spaces).
	cmd := wimlibCmd(wimlib, "update", wim, strconv.Itoa(index),
		"--command=add "+hiveFile+" /"+hiveInWIM)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
