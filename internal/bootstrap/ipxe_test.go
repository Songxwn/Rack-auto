package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallIPXEOffline(t *testing.T) {
	dir := t.TempDir()
	if err := InstallIPXE(dir); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"undionly.kpxe", "ipxe.efi", "snponly.efi"} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil || st.Size() < 1024 {
			t.Fatalf("%s: %v", n, err)
		}
	}
}
