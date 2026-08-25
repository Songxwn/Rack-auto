package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsAutoMirror(t *testing.T) {
	if !isAutoMirror("") || !isAutoMirror(" auto ") || !isAutoMirror("AUTO") {
		t.Fatal("auto")
	}
	if isAutoMirror("https://mirrors.aliyun.com/ubuntu-releases") {
		t.Fatal("pinned")
	}
}

func TestPickFastestMirror(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			http.NotFound(w, r)
			return
		}
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 *ubuntu-26.04-live-server-amd64.iso\n"))
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 *ubuntu-26.04-live-server-amd64.iso\n"))
	}))
	defer fast.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 404)
	}))
	defer dead.Close()

	mirrors := []ubuntuMirror{
		{Name: "slow", Root: slow.URL, Kind: kindReleases},
		{Name: "fast", Root: fast.URL, Kind: kindReleases},
		{Name: "dead", Root: dead.URL, Kind: kindReleases},
	}
	hit := pickFastestMirror(http.DefaultClient, mirrors, "26.04")
	if hit.err != nil {
		t.Fatal(hit.err)
	}
	if hit.name != "fast" {
		t.Fatalf("got %s", hit.name)
	}
	if !strings.Contains(hit.sums, "live-server-amd64.iso") {
		t.Fatalf("sums %s", hit.sums)
	}
}

func TestUbuntuMirrorISODir(t *testing.T) {
	r := ubuntuMirror{Root: "https://mirrors.example/ubuntu-releases", Kind: kindReleases}
	if r.isoDir("26.04") != "https://mirrors.example/ubuntu-releases/26.04" {
		t.Fatal(r.isoDir("26.04"))
	}
	c := ubuntuMirror{Root: "https://mirrors.example/ubuntu-cdimage/releases", Kind: kindCDImage}
	if c.isoDir("26.04") != "https://mirrors.example/ubuntu-cdimage/releases/26.04/release" {
		t.Fatal(c.isoDir("26.04"))
	}
}
