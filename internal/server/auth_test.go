package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/server"
	"github.com/Songxwn/Rack-auto/internal/store"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return server.New(cfg, st, netboot.New(cfg, st)).Handler()
}

func doReq(h http.Handler, method, path, body, cookie string, hdr map[string]string) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookieFrom(rec *httptest.ResponseRecorder) string {
	var parts []string
	for _, c := range rec.Result().Cookies() {
		if c.Value == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func TestWebLoginGatesConsoleAllowsIpxeAndAgent(t *testing.T) {
	h := testHandler(t)

	rec := doReq(h, "GET", "/api/v1/machines", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("machines without login: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "GET", "/api/v1/health", "", "", nil)
	if rec.Code != 200 {
		t.Fatalf("health: %d", rec.Code)
	}
  rec = doReq(h, "GET", "/i18n.js", "", "", nil)
  if rec.Code != 200 {
    t.Fatalf("web i18n without login: %d", rec.Code)
  }

	rec = doReq(h, "GET", "/ipxe/boot.ipxe", "", "", nil)
	if rec.Code != 200 {
		t.Fatalf("ipxe without login should be allowed: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "GET", "/winpe/wimboot", "", "", nil)
	if rec.Code != 200 || rec.Body.Len() < 1024 {
		t.Fatalf("wimboot without login: %d len=%d %s", rec.Code, rec.Body.Len(), rec.Body.String())
	}

	rec = doReq(h, "POST", "/api/v1/agent/register", `{"mac":"aa:bb:cc:dd:ee:11"}`, "", nil)
	if rec.Code != 200 && rec.Code != 201 {
		t.Fatalf("agent register without web login: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "POST", "/api/v1/login", `{"username":"admin","password":"wrong"}`, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d", rec.Code)
	}

	rec = doReq(h, "POST", "/api/v1/login", `{"username":"admin","password":"admin"}`, "", nil)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	ck := cookieFrom(rec)
	if ck == "" {
		t.Fatal("missing session cookie")
	}

	rec = doReq(h, "GET", "/api/v1/session", "", ck, nil)
	var sess map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil || sess["authenticated"] != true {
		t.Fatalf("session: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "GET", "/api/v1/machines", "", ck, nil)
	if rec.Code != 200 {
		t.Fatalf("machines after login: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "PUT", "/api/v1/account", `{"current_password":"admin","username":"ops","password":"newpass"}`, ck, nil)
	if rec.Code != 200 {
		t.Fatalf("account: %d %s", rec.Code, rec.Body.String())
	}
	ck = cookieFrom(rec)

	rec = doReq(h, "POST", "/api/v1/login", `{"username":"ops","password":"newpass"}`, "", nil)
	if rec.Code != 200 {
		t.Fatalf("login after change: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "POST", "/api/v1/login", `{"username":"admin","password":"admin"}`, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password should fail: %d", rec.Code)
	}

	rec = doReq(h, "POST", "/api/v1/logout", "{}", ck, nil)
	if rec.Code != 200 {
		t.Fatalf("logout: %d", rec.Code)
	}
	rec = doReq(h, "GET", "/api/v1/machines", "", ck, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("machines after logout: %d", rec.Code)
	}
}

func TestAPITokenStillOpensConsole(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.APIToken = "script-token"
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_ = st.SetSetting("api_token", cfg.APIToken)
	h := server.New(cfg, st, netboot.New(cfg, st)).Handler()

	rec := doReq(h, "GET", "/api/v1/machines", "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie no token: %d", rec.Code)
	}
	rec = doReq(h, "GET", "/api/v1/machines", "", "", map[string]string{"X-API-Token": "script-token"})
	if rec.Code != 200 {
		t.Fatalf("token should open console API: %d %s", rec.Code, rec.Body.String())
	}
}
