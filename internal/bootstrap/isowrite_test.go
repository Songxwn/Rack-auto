package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackCasperISOGoRoundtrip(t *testing.T) {
	src := t.TempDir()
	casper := filepath.Join(src, "casper")
	disk := filepath.Join(src, ".disk")
	if err := os.MkdirAll(casper, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(disk, 0o755); err != nil {
		t.Fatal(err)
	}
	long := "ubuntu-server-minimal.ubuntu-server.installer.generic.squashfs"
	sq := []byte("squashfs-payload-bytes")
	if err := os.WriteFile(filepath.Join(casper, long), sq, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disk, "info"), []byte("Ubuntu RAMOS"), 0o644); err != nil {
		t.Fatal(err)
	}

	iso := filepath.Join(t.TempDir(), "casper.iso")
	if err := packCasperISOGo(src, iso); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(iso)
	if err != nil || st.Size() < isoSector*20 {
		t.Fatalf("iso size %v %v", st, err)
	}

	out := t.TempDir()
	if err := ExtractISOPrefix(iso, out, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "casper", long))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sq) {
		t.Fatalf("squashfs %q", got)
	}
	info, err := os.ReadFile(filepath.Join(out, ".disk", "info"))
	if err != nil {
		t.Fatal(err)
	}
	if string(info) != "Ubuntu RAMOS" {
		t.Fatalf("info %q", info)
	}
}

func TestExtractISOPrefixKeepsSquashfs(t *testing.T) {
	iso := writeTestISO(t, map[string][]byte{
		"casper/vmlinuz":      []byte("kernel-bytes-here"),
		"casper/layer.squashfs": []byte("sq-bytes"),
	})
	dest := t.TempDir()
	if err := ExtractISOPrefix(iso, dest, keepCasperLive); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "casper", "layer.squashfs")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "casper", "vmlinuz")); err == nil {
		t.Fatal("vmlinuz should be skipped")
	}
}
