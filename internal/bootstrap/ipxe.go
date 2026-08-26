package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Songxwn/Rack-auto/internal/bootstrap/embedded"
)

// InstallIPXE copies bundled iPXE firmware into the TFTP directory.
// Existing non-empty files are left unchanged so operators can replace them.
func InstallIPXE(tftpDir string) error {
	if err := os.MkdirAll(tftpDir, 0o755); err != nil {
		return err
	}
	for dstName, srcName := range embedded.Files {
		dst := filepath.Join(tftpDir, dstName)
		if st, err := os.Stat(dst); err == nil && st.Size() > 1024 {
			continue
		}
		b, err := embedded.FS.ReadFile(srcName)
		if err != nil {
			return fmt.Errorf("embedded iPXE %s: %w", srcName, err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		fmt.Println("   +", dst, "(bundled)")
	}
	return nil
}

func ReadWimboot() ([]byte, error) {
	b, err := embedded.FS.ReadFile("wimboot")
	if err != nil {
		return nil, fmt.Errorf("embedded wimboot: %w", err)
	}
	if len(b) < 1024 {
		return nil, fmt.Errorf("embedded wimboot is truncated")
	}
	return b, nil
}
