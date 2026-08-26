package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/cryptpw"
)

const (
	sessionCookie     = "rackauto_session"
	sessionTTL        = 7 * 24 * time.Hour
	defaultWebUser    = "admin"
	defaultWebPass    = "admin"
	webUserKey        = "web_username"
	webPassKey        = "web_password_hash"
	loginFailLimit    = 8
	loginFailLock     = 30 * time.Second
	maxUsernameLen    = 64
	maxPasswordLen    = 256
)

type webSession struct {
	username string
	expiry   time.Time
}

type loginGuard struct {
	fails int
	until time.Time
}

func (s *Server) ensureWebAccount() error {
	if s.Store == nil {
		return fmt.Errorf("no store")
	}
	if strings.TrimSpace(s.Store.Setting(webUserKey, "")) == "" {
		if err := s.Store.SetSetting(webUserKey, defaultWebUser); err != nil {
			return err
		}
	}
	if s.Store.Setting(webPassKey, "") != "" {
		return nil
	}
	h, err := cryptpw.SHA512(defaultWebPass)
	if err != nil {
		return err
	}
	return s.Store.SetSetting(webPassKey, h)
}

func (s *Server) webUser() string {
	u := strings.TrimSpace(s.Store.Setting(webUserKey, defaultWebUser))
	if u == "" {
		return defaultWebUser
	}
	return u
}

func (s *Server) webAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.sessionUser(r); ok {
			next(w, r)
			return
		}
		if s.apiTokenOK(r) {
			next(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

func (s *Server) apiTokenOK(r *http.Request) bool {
	tok := s.token()
	if tok == "" {
		return false
	}
	got := r.Header.Get("X-API-Token")
	if got == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			got = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	return got != "" && got == tok
}

func (s *Server) sessionUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	sess, ok := s.sessions[c.Value]
	if !ok || time.Now().After(sess.expiry) {
		delete(s.sessions, c.Value)
		return "", false
	}
	sess.expiry = time.Now().Add(sessionTTL)
	s.sessions[c.Value] = sess
	return sess.username, true
}

func (s *Server) newSession(username string) string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		b = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	id := hex.EncodeToString(b)
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	now := time.Now()
	for k, v := range s.sessions {
		if now.After(v.expiry) {
			delete(s.sessions, k)
		}
	}
	s.sessions[id] = webSession{username: username, expiry: now.Add(sessionTTL)}
	return id
}

func (s *Server) dropSessions() {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	s.sessions = map[string]webSession{}
}

func (s *Server) dropSession(id string) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	delete(s.sessions, id)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, id string, maxAge int) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		c.Secure = true
	}
	http.SetCookie(w, c)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) loginLocked(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	g, ok := s.loginFails[ip]
	if !ok {
		return false
	}
	if time.Now().After(g.until) && g.fails >= loginFailLimit {
		delete(s.loginFails, ip)
		return false
	}
	return g.fails >= loginFailLimit && time.Now().Before(g.until)
}

func (s *Server) noteLogin(ip string, ok bool) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if ok {
		delete(s.loginFails, ip)
		return
	}
	g := s.loginFails[ip]
	g.fails++
	if g.fails >= loginFailLimit {
		g.until = time.Now().Add(loginFailLock)
	}
	s.loginFails[ip] = g
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.loginLocked(ip) {
		http.Error(w, "too many attempts, try later", http.StatusTooManyRequests)
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	user := strings.TrimSpace(in.Username)
	pass := in.Password
	wantUser := s.webUser()
	hash := s.Store.Setting(webPassKey, "")
	if user == "" || pass == "" || user != wantUser || !cryptpw.Compare(hash, pass) {
		s.noteLogin(ip, false)
		time.Sleep(180 * time.Millisecond)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	s.noteLogin(ip, true)
	id := s.newSession(wantUser)
	s.setSessionCookie(w, r, id, int(sessionTTL.Seconds()))
	s.Store.AddEvent("info", "控制台登录 "+wantUser, "")
	writeJSON(w, 200, map[string]any{"ok": true, "username": wantUser})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		s.dropSession(c.Value)
	}
	s.setSessionCookie(w, r, "", -1)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	if u, ok := s.sessionUser(r); ok {
		writeJSON(w, 200, map[string]any{"authenticated": true, "username": u})
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": false})
}

func (s *Server) putAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		CurrentPassword string `json:"current_password"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if in.CurrentPassword == "" {
		http.Error(w, "current_password required", 400)
		return
	}
	hash := s.Store.Setting(webPassKey, "")
	if !cryptpw.Compare(hash, in.CurrentPassword) {
		http.Error(w, "current password incorrect", 401)
		return
	}
	user := strings.TrimSpace(in.Username)
	if user == "" {
		user = s.webUser()
	}
	if len(user) > maxUsernameLen {
		http.Error(w, "username too long", 400)
		return
	}
	passChanged := strings.TrimSpace(in.Password) != ""
	if passChanged {
		if len(in.Password) > maxPasswordLen {
			http.Error(w, "password too long", 400)
			return
		}
		h, err := cryptpw.SHA512(in.Password)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := s.Store.SetSetting(webPassKey, h); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if err := s.Store.SetSetting(webUserKey, user); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.dropSessions()
	id := s.newSession(user)
	s.setSessionCookie(w, r, id, int(sessionTTL.Seconds()))
	s.Store.AddEvent("info", "更新控制台账号 "+user, "")
	writeJSON(w, 200, map[string]any{"ok": true, "username": user})
}
