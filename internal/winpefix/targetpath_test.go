package winpefix_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/Songxwn/Rack-auto/internal/winpefix"
)

func utf16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[i*2] = byte(c)
		b[i*2+1] = byte(c >> 8)
	}
	return b
}

func TestPatchSOFTWAREHiveTargetPath(t *testing.T) {
	// Fake hive blob containing Setup paths (plus noise).
	var raw []byte
	raw = append(raw, bytes.Repeat([]byte{0xDE, 0xAD}, 32)...)
	raw = append(raw, utf16LE(`X:\$windows.~bt\Windows`)...)
	raw = append(raw, 0, 0)
	raw = append(raw, utf16LE(`X:\$windows.~bt\`)...)
	raw = append(raw, 0, 0)
	raw = append(raw, bytes.Repeat([]byte{0xBE, 0xEF}, 16)...)

	out, n := winpefix.PatchSOFTWAREHive(raw)
	if n < 2 {
		t.Fatalf("expected rewrites, got %d", n)
	}
	if bytes.Contains(out, utf16LE(`$windows.~bt`)) || bytes.Contains(out, utf16LE(`$Windows.~BT`)) {
		t.Fatal("setup target path marker still present")
	}
	if !bytes.Contains(out, utf16LE(`X:\Windows`)) {
		t.Fatal("missing X:\\Windows")
	}
	if !bytes.Contains(out, utf16LE(`X:\`)) {
		t.Fatal("missing X:\\")
	}
	if len(out) != len(raw) {
		t.Fatalf("length must stay %d got %d", len(raw), len(out))
	}
}

func TestFixBootWIMMissingTool(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "boot.wim")
	if err := os.WriteFile(p, []byte("MSWIM\x00\x00\x00not-real"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Prepend a fake PATH with no wimlib.
	t.Setenv("PATH", dir)
	err := winpefix.FixBootWIM(p)
	if err == nil {
		t.Fatal("expected error without wimlib-imagex")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("wimlib-imagex")) {
		t.Fatalf("err=%v", err)
	}
}
