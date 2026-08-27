package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/server"
	"github.com/Songxwn/Rack-auto/internal/store"
)

func TestChunkedImageUpload(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PublicURL = "http://127.0.0.1:8080"
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := server.New(cfg, st, netboot.New(cfg, st)).Handler()

	login := doReq(h, "POST", "/api/v1/login", `{"username":"admin","password":"admin"}`, "", nil)
	if login.Code != 200 {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
	cookie := cookieFrom(login)

	payload := bytes.Repeat([]byte("ISO-DATA-"), 1024)
	initBody, _ := json.Marshal(map[string]any{
		"name": "test.iso", "filename": "test.iso", "size": len(payload),
		"kind": "windows-iso", "os_family": "windows", "os_version": "2022",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/images/upload/init", bytes.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("init %d %s", rec.Code, rec.Body.String())
	}
	var meta map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &meta)
	id, _ := meta["id"].(string)
	if id == "" {
		t.Fatal(meta)
	}

	chunk := 4096
	for off := 0; off < len(payload); off += chunk {
		end := off + chunk
		if end > len(payload) {
			end = len(payload)
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("PUT", "/api/v1/images/upload/"+id+"/chunk", bytes.NewReader(payload[off:end]))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Cookie", cookie)
		req.Header.Set("X-Upload-Offset", fmt.Sprintf("%d", off))
		req.ContentLength = int64(end - off)
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("chunk @%d %d %s", off, rec.Code, rec.Body.String())
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/images/upload/"+id+"/complete", nil)
	req.Header.Set("Cookie", cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("complete %d %s", rec.Code, rec.Body.String())
	}
	var img map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &img)
	fn, _ := img["filename"].(string)
	if fn == "" {
		t.Fatal(img)
	}
	got, err := os.ReadFile(filepath.Join(cfg.ImagesDir(), fn))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch %d vs %d", len(got), len(payload))
	}
}
