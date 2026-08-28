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
	name, sum, err := parseLiveServerISO(sums, "26.04", "amd64")
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

func TestParseLiveServerISOPrefersPointRelease(t *testing.T) {
	sums := `
dec49008a71f6098d0bcfc822021f4d042d5f2db279e4d75bdd981304f1ca5d9 *ubuntu-26.04-live-server-amd64.iso
cc8a95cde20f6ced61a322420de00f10cc3c90ced545daa46cb9c1a117f1d927 *ubuntu-26.04.1-live-server-amd64.iso
`
	name, sum, err := parseLiveServerISO(sums, "26.04.1", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "ubuntu-26.04.1-live-server-amd64.iso" {
		t.Fatalf("name %s", name)
	}
	if sum != "cc8a95cde20f6ced61a322420de00f10cc3c90ced545daa46cb9c1a117f1d927" {
		t.Fatalf("sum %s", sum)
	}
}

func TestParseLiveServerISOPicksLatestWhenSeriesOnly(t *testing.T) {
	sums := `
dec49008a71f6098d0bcfc822021f4d042d5f2db279e4d75bdd981304f1ca5d9 *ubuntu-26.04-live-server-amd64.iso
cc8a95cde20f6ced61a322420de00f10cc3c90ced545daa46cb9c1a117f1d927 *ubuntu-26.04.1-live-server-amd64.iso
`
	name, _, err := parseLiveServerISO(sums, "26.04", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "ubuntu-26.04.1-live-server-amd64.iso" {
		t.Fatalf("name %s", name)
	}
}

func TestAdjustISOBase(t *testing.T) {
	base := "https://mirror.twds.com.tw/ubuntu-releases/26.04"
	got := adjustISOBase(base, "ubuntu-26.04.1-live-server-amd64.iso")
	want := "https://mirror.twds.com.tw/ubuntu-releases/26.04.1"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	arm := "https://cdimage.ubuntu.com/releases/26.04/release"
	got = adjustISOBase(arm, "ubuntu-26.04.1-live-server-arm64.iso")
	want = "https://cdimage.ubuntu.com/releases/26.04.1/release"
	if got != want {
		t.Fatalf("arm got %s want %s", got, want)
	}
}

func TestReleaseProbeDirs(t *testing.T) {
	got := releaseProbeDirs("26.04")
	if len(got) != 2 || got[0] != "26.04.1" || got[1] != "26.04" {
		t.Fatalf("%v", got)
	}
	if dirs := releaseProbeDirs("26.04.1"); len(dirs) != 1 || dirs[0] != "26.04.1" {
		t.Fatalf("%v", dirs)
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

