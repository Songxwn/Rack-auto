package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/osprofile"
)

const (
	uploadMaxBytes     = 64 << 30 // 64 GiB
	uploadStaleAfter   = 48 * time.Hour
	uploadChunkMaxSize = 64 << 20 // 64 MiB per chunk
)

type imageUploadMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	Received  int64     `json:"received"`
	Kind      string    `json:"kind"`
	OSFamily  string    `json:"os_family"`
	OSVersion string    `json:"os_version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

var uploadMu sync.Mutex

func (s *Server) uploadsDir() string {
	return filepath.Join(s.Cfg.ImagesDir(), ".uploads")
}

func (s *Server) uploadMetaPath(id string) string {
	return filepath.Join(s.uploadsDir(), id+".json")
}

func (s *Server) uploadPartPath(id string) string {
	return filepath.Join(s.uploadsDir(), id+".part")
}

func (s *Server) loadUploadMeta(id string) (imageUploadMeta, error) {
	id = sanitizeUploadID(id)
	if id == "" {
		return imageUploadMeta{}, fmt.Errorf("invalid upload id")
	}
	b, err := os.ReadFile(s.uploadMetaPath(id))
	if err != nil {
		return imageUploadMeta{}, err
	}
	var m imageUploadMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return imageUploadMeta{}, err
	}
	return m, nil
}

func (s *Server) saveUploadMeta(m imageUploadMeta) error {
	if err := os.MkdirAll(s.uploadsDir(), 0o755); err != nil {
		return err
	}
	m.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.uploadMetaPath(m.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.uploadMetaPath(m.ID))
}

func sanitizeUploadID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return ""
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return id
}

func (s *Server) cleanupStaleUploads() {
	dir := s.uploadsDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-uploadStaleAfter)
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		m, err := s.loadUploadMeta(id)
		if err != nil {
			continue
		}
		if m.UpdatedAt.Before(cutoff) && m.CreatedAt.Before(cutoff) {
			_ = os.Remove(s.uploadMetaPath(id))
			_ = os.Remove(s.uploadPartPath(id))
		}
	}
}

func (s *Server) initImageUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Filename  string `json:"filename"`
		Size      int64  `json:"size"`
		Kind      string `json:"kind"`
		OSFamily  string `json:"os_family"`
		OSVersion string `json:"os_version"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Size <= 0 || req.Size > uploadMaxBytes {
		http.Error(w, "invalid size", 400)
		return
	}
	fn := filepath.Base(strings.TrimSpace(req.Filename))
	if fn == "" || fn == "." || fn == ".." {
		http.Error(w, "filename required", 400)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fn
	}
	s.cleanupStaleUploads()
	id := RandHex(16)
	if id == "" {
		http.Error(w, "id generation failed", 500)
		return
	}
	if err := os.MkdirAll(s.uploadsDir(), 0o755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	part := s.uploadPartPath(id)
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_ = f.Close()
	m := imageUploadMeta{
		ID:        id,
		Name:      name,
		Filename:  fn,
		Size:      req.Size,
		Received:  0,
		Kind:      req.Kind,
		OSFamily:  req.OSFamily,
		OSVersion: req.OSVersion,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.saveUploadMeta(m); err != nil {
		_ = os.Remove(part)
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 201, m)
}

func (s *Server) getImageUpload(w http.ResponseWriter, r *http.Request) {
	m, err := s.loadUploadMeta(r.PathValue("id"))
	if err != nil {
		http.Error(w, "upload not found", 404)
		return
	}
	if st, err := os.Stat(s.uploadPartPath(m.ID)); err == nil {
		if st.Size() != m.Received {
			m.Received = st.Size()
			_ = s.saveUploadMeta(m)
		}
	}
	writeJSON(w, 200, m)
}

func (s *Server) putImageUploadChunk(w http.ResponseWriter, r *http.Request) {
	id := sanitizeUploadID(r.PathValue("id"))
	if id == "" {
		http.Error(w, "invalid upload id", 400)
		return
	}
	uploadMu.Lock()
	defer uploadMu.Unlock()

	m, err := s.loadUploadMeta(id)
	if err != nil {
		http.Error(w, "upload not found", 404)
		return
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(r.Header.Get("X-Upload-Offset")), 10, 64)
	if err != nil || offset < 0 {
		http.Error(w, "X-Upload-Offset required", 400)
		return
	}
	if offset != m.Received {
		http.Error(w, fmt.Sprintf("offset mismatch: want %d got %d", m.Received, offset), 409)
		return
	}
	if r.ContentLength < 0 {
		http.Error(w, "Content-Length required", 400)
		return
	}
	if r.ContentLength == 0 {
		http.Error(w, "empty chunk", 400)
		return
	}
	if r.ContentLength > uploadChunkMaxSize {
		http.Error(w, "chunk too large", 413)
		return
	}
	if offset+r.ContentLength > m.Size {
		http.Error(w, "chunk exceeds declared size", 400)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, r.ContentLength+1024)

	part := s.uploadPartPath(m.ID)
	f, err := os.OpenFile(part, os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, r.ContentLength))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if n != r.ContentLength {
		http.Error(w, fmt.Sprintf("short chunk write %d/%d", n, r.ContentLength), 400)
		return
	}
	if err := f.Sync(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	m.Received = offset + n
	if err := s.saveUploadMeta(m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":       m.ID,
		"received": m.Received,
		"size":     m.Size,
	})
}

func (s *Server) completeImageUpload(w http.ResponseWriter, r *http.Request) {
	id := sanitizeUploadID(r.PathValue("id"))
	if id == "" {
		http.Error(w, "invalid upload id", 400)
		return
	}
	uploadMu.Lock()
	defer uploadMu.Unlock()

	m, err := s.loadUploadMeta(id)
	if err != nil {
		http.Error(w, "upload not found", 404)
		return
	}
	part := s.uploadPartPath(m.ID)
	st, err := os.Stat(part)
	if err != nil {
		http.Error(w, "upload data missing", 400)
		return
	}
	if st.Size() != m.Size || m.Received != m.Size {
		http.Error(w, fmt.Sprintf("incomplete upload: %d/%d", st.Size(), m.Size), 400)
		return
	}
	dstName := fmt.Sprintf("%d-%s", time.Now().Unix(), m.Filename)
	dst := filepath.Join(s.Cfg.ImagesDir(), dstName)
	if err := os.MkdirAll(s.Cfg.ImagesDir(), 0o755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.Rename(part, dst); err != nil {
		// cross-device fallback
		if err := copyFile(part, dst); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = os.Remove(part)
	}
	_ = os.Remove(s.uploadMetaPath(m.ID))

	img := model.Image{
		Name:      m.Name,
		OSFamily:  m.OSFamily,
		OSVersion: m.OSVersion,
		Kind:      m.Kind,
		URL:       s.Store.Setting("public_url", s.Cfg.PublicURL) + "/images/" + dstName,
		Filename:  dstName,
		SizeB:     m.Size,
	}
	if img.Kind == "" {
		if osprofile.IsWindows(img.OSFamily) {
			img.Kind = model.ImageWindowsISO
		} else {
			img.Kind = model.ImageCloudDisk
		}
	}
	img.OSFamily = osprofile.CanonicalFamily(img.OSFamily)
	if img.OSVersion == "" {
		img.OSVersion = osprofile.Lookup(img.OSFamily, "").ID
	}
	s.fillImageInspect(&img)
	if err := s.Store.UpsertImage(&img); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.Store.AddEvent("info", "上传镜像 "+img.Name, "")
	writeJSON(w, 201, img)
}

func (s *Server) abortImageUpload(w http.ResponseWriter, r *http.Request) {
	id := sanitizeUploadID(r.PathValue("id"))
	if id == "" {
		http.Error(w, "invalid upload id", 400)
		return
	}
	uploadMu.Lock()
	defer uploadMu.Unlock()
	_ = os.Remove(s.uploadMetaPath(id))
	_ = os.Remove(s.uploadPartPath(id))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return out.Sync()
}
