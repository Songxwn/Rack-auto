package winpecurl_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/winpecurl"
)

func TestMainGETAndPOST(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "body.json")
	if err := os.WriteFile(payload, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotAuth, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file":
			w.Write([]byte("wim-bytes"))
		case "/progress":
			gotAuth = r.Header.Get("X-API-Token")
			gotCT = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(200)
		case "/complete":
			b, _ := io.ReadAll(r.Body)
			if string(b) != `{"ok":true}` {
				http.Error(w, "bad body", 400)
				return
			}
			w.WriteHeader(200)
		case "/missing":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out := filepath.Join(dir, "payload.bin")
	if code := winpecurl.Main([]string{"-fL", "--retry", "2", "--retry-delay", "0", "-o", out, srv.URL + "/file"}); code != 0 {
		t.Fatalf("get code %d", code)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "wim-bytes" {
		t.Fatalf("got %q %v", b, err)
	}

	if code := winpecurl.Main([]string{
		"-fL", "-H", "X-API-Token: secret", "-H", "Content-Type: application/json",
		"-d", `{"progress":20,"message":"downloading_wim"}`, srv.URL + "/progress",
	}); code != 0 {
		t.Fatalf("post code %d", code)
	}
	if gotAuth != "secret" || !strings.Contains(gotCT, "json") || !strings.Contains(gotBody, "downloading_wim") {
		t.Fatalf("headers %q %q body %q", gotAuth, gotCT, gotBody)
	}

	if code := winpecurl.Main([]string{"-fL", "--data-binary", "@" + payload, srv.URL + "/complete"}); code != 0 {
		t.Fatalf("data-binary code %d", code)
	}
	if code := winpecurl.Main([]string{"-fL", "-o", filepath.Join(dir, "x"), srv.URL + "/missing"}); code != 22 {
		t.Fatalf("fail code %d", code)
	}
}

func TestRejectsHTMLSavedAsWIM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!doctype html><title>login</title>"))
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "install.wim")
	if code := winpecurl.Main([]string{"-fL", "-o", out, srv.URL}); code == 0 {
		t.Fatal("HTML must not be accepted as a WIM")
	}
}
