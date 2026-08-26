package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/config"
)

func TestParseLiveServerISO(t *testing.T) {
	sums := `
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 *ubuntu-26.04-live-server-amd64.iso
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa *ubuntu-26.04-live-server-amd64.iso.zsync
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb *ubuntu-26.04-live-server-amd64.list
`
	name, sum, err := parseLiveServerISO(sums, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "ubuntu-26.04-live-server-amd64.iso" {
		t.Fatalf("name %s", name)
	}
	if sum != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("sum %s", sum)
	}
}

func TestUbuntuISOBase(t *testing.T) {
	cfg := config.Config{}
	cfg.Bootstrap.UbuntuMirror = ""
	got := ubuntuISOBase(cfg, "26.04", "amd64")
	if got != "https://releases.ubuntu.com/26.04" {
		t.Fatalf("official %s", got)
	}
	cfg.Bootstrap.UbuntuMirror = "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases/"
	got = ubuntuISOBase(cfg, "26.04", "amd64")
	if got != "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases/26.04" {
		t.Fatalf("mirror %s", got)
	}
	got = ubuntuISOBase(cfg, "26.04", "arm64")
	if !strings.Contains(got, "cdimage.ubuntu.com") || !strings.HasSuffix(got, "/26.04/release") {
		t.Fatalf("arm %s", got)
	}
}

func TestUbuntuDebArch(t *testing.T) {
	a, err := ubuntuDebArch("x86_64")
	if err != nil || a != "amd64" {
		t.Fatalf("%s %v", a, err)
	}
	a, err = ubuntuDebArch("aarch64")
	if err != nil || a != "arm64" {
		t.Fatalf("%s %v", a, err)
	}
}

func TestKeepCasperLive(t *testing.T) {
	if !keepCasperLive("casper/ubuntu-server-minimal.squashfs") {
		t.Fatal("squashfs")
	}
	if keepCasperLive("casper/vmlinuz") || keepCasperLive("casper/initrd") {
		t.Fatal("kernel files")
	}
	if !keepCasperLive(".disk/info") {
		t.Fatal(".disk")
	}
}

func TestWriteLayerFSPathPicksLongest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ubuntu-server-minimal.squashfs"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	long := "ubuntu-server-minimal.ubuntu-server.installer.generic.squashfs"
	if err := os.WriteFile(filepath.Join(dir, long), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "layerfs-path")
	if err := writeLayerFSPath(dir, dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != long {
		t.Fatalf("got %q", b)
	}
}

