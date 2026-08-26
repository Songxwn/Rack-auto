package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksQcow(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(q, []byte("QFI\xfbrest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !looksQcow(q) {
		t.Fatal("expected qcow magic")
	}
	raw := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(raw, []byte("notqcow"), 0o644); err != nil {
		t.Fatal(err)
	}
	if looksQcow(raw) {
		t.Fatal("raw should not look like qcow")
	}
}
