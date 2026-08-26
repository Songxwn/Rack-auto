package winpecurl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Songxwn/Rack-auto/internal/config"
)

func okFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.Size() < 1024 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [2]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	return hdr[0] == 'M' && hdr[1] == 'Z'
}

func findBesideExe() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	for _, dir := range dirs {
		for _, name := range []string{"winpe-curl.exe", "curl.exe"} {
			p := filepath.Join(dir, name)
			if okFile(p) {
				return p
			}
		}
	}
	return ""
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	_ = os.Remove(dest)
	return os.Rename(tmp, dest)
}

func Dest(cfg config.Config) string {
	return filepath.Join(cfg.WinPEDir(), "curl.exe")
}

// Path is the curl.exe the control plane should serve to wimboot.
func Path(cfg config.Config) string {
	if p := Dest(cfg); okFile(p) {
		return p
	}
	return findBesideExe()
}

// Install copies Release winpe-curl.exe next to rackauto into data/winpe/curl.exe.
func Install(cfg config.Config) error {
	if err := os.MkdirAll(cfg.WinPEDir(), 0o755); err != nil {
		return err
	}
	dest := Dest(cfg)
	src := findBesideExe()
	if src == "" {
		if okFile(dest) {
			return nil
		}
		return fmt.Errorf("winpe-curl.exe not found (put it next to rackauto or in %s)", dest)
	}
	if src == dest {
		return nil
	}
	ss, err := os.Stat(src)
	if err != nil {
		return err
	}
	if ds, err := os.Stat(dest); err == nil && ds.Size() == ss.Size() {
		return nil
	}
	return copyFile(src, dest)
}
