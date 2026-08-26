package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/bootstrap"
	"github.com/Songxwn/Rack-auto/internal/imageinspect"
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/provision"
	"github.com/Songxwn/Rack-auto/internal/store"
	"github.com/Songxwn/Rack-auto/internal/winpecurl"
)

func (s *Server) coerceWindowsImage(img *model.Image) {
	if img == nil || img.Inspect == nil || !img.Inspect.Windows {
		return
	}
	img.OSFamily = "windows"
	if ver := imageinspect.DetectWindowsVersion(img.Inspect.WIMImages); ver != "" {
		img.OSVersion = ver
	}
	if !model.IsWindowsKind(img.Kind) {
		if img.Inspect.Format == "iso" {
			img.Kind = model.ImageWindowsISO
		} else {
			img.Kind = model.ImageWindowsWIM
		}
	}
}

func (s *Server) materializeWindows(img *model.Image, src string) {
	if img == nil || src == "" {
		return
	}
	if img.ID == "" {
		img.ID = store.NewID("img")
	}
	if img.Inspect == nil {
		return
	}
	in := img.Inspect
	destDir := filepath.Join(s.Cfg.ImagesDir(), "win", img.ID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		in.Warnings = append(in.Warnings, "mkdir win payload: "+err.Error())
		return
	}
	bootDst := filepath.Join(destDir, "boot.wim")
	installName := "install.wim"
	if in.InstallWIM != "" {
		base := filepath.Base(in.InstallWIM)
		if base != "." && base != string(filepath.Separator) {
			installName = base
		}
	}

	if in.Format == "iso" || img.Kind == model.ImageWindowsISO {
		if err := bootstrap.ExtractISOFiles(src, map[string]string{"sources/boot.wim": bootDst}); err != nil {
			if exts, e2 := imageinspect.FindWIMExtentsFile(src); e2 == nil {
				for _, e := range exts {
					if e.IsBootWIM() {
						if err := bootstrap.CopyFileRange(src, e.Offset, e.Size, bootDst); err != nil {
							in.Warnings = append(in.Warnings, "extract boot.wim: "+err.Error())
						}
						break
					}
				}
			} else {
				in.Warnings = append(in.Warnings, "extract boot.wim: "+err.Error())
			}
		}
		if in.InstallOff == 0 && in.InstallSize == 0 {
			if exts, err := imageinspect.FindWIMExtentsFile(src); err == nil {
				for _, e := range exts {
					if e.IsBootWIM() {
						continue
					}
					in.InstallOff = e.Offset
					in.InstallSize = e.Size
					if len(e.Images) > 0 {
						in.WIMImages = e.Images
					}
					break
				}
			}
		}
		in.InstallFrom = img.Filename
		if in.InstallFrom == "" {
			in.InstallFrom = filepath.Base(src)
		}
		in.InstallWIM = "win/" + img.ID + "/" + installName
	} else {
		st, err := os.Stat(src)
		if err == nil {
			in.InstallFrom = img.Filename
			if in.InstallFrom == "" {
				in.InstallFrom = filepath.Base(src)
			}
			in.InstallOff = 0
			in.InstallSize = st.Size()
			in.InstallWIM = "win/" + img.ID + "/" + installName
		}
		if _, err := os.Stat(bootDst); err != nil {
			if err := s.borrowBootWIM(img.OSVersion, bootDst); err != nil {
				in.Warnings = append(in.Warnings, err.Error())
				if in.Status == "ok" {
					in.Status = "warn"
				}
			}
		}
	}

	if _, err := os.Stat(bootDst); err != nil && (in.Format == "iso" || img.Kind == model.ImageWindowsISO) {
		if exts, err := imageinspect.FindWIMExtentsFile(src); err == nil && len(exts) >= 2 {
			smallest := exts[0]
			for _, e := range exts[1:] {
				if e.Size > 0 && e.Size < smallest.Size {
					smallest = e
				}
			}
			if smallest.Size > 0 && smallest.Offset != in.InstallOff {
				_ = bootstrap.CopyFileRange(src, smallest.Offset, smallest.Size, bootDst)
			}
		}
	}

	if st, err := os.Stat(bootDst); err == nil && st.Size() > 0 {
		in.BootWIM = "win/" + img.ID + "/boot.wim"
	} else if in.BootWIM == "" {
		in.Warnings = append(in.Warnings, "WinPE boot.wim missing; upload a Windows Server ISO of the same generation")
		if in.Status == "ok" {
			in.Status = "warn"
		}
	}
	if in.InstallSize > 0 && in.Message == "" {
		in.Message = "Windows install media ready"
	}
}

func (s *Server) borrowBootWIM(version, dest string) error {
	list, err := s.Store.ListImages()
	if err != nil {
		return fmt.Errorf("no WinPE boot.wim to borrow: %w", err)
	}
	var fallback string
	for _, img := range list {
		if !img.IsWindows() || img.Inspect == nil || img.Inspect.BootWIM == "" {
			continue
		}
		p := filepath.Join(s.Cfg.ImagesDir(), img.Inspect.BootWIM)
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			p = filepath.Join(s.Cfg.ImagesDir(), "win", img.ID, "boot.wim")
			if st, err = os.Stat(p); err != nil || st.Size() == 0 {
				continue
			}
		}
		if version != "" && img.OSVersion == version {
			return copyFile(p, dest)
		}
		if fallback == "" {
			fallback = p
		}
	}
	if fallback != "" {
		return copyFile(fallback, dest)
	}
	return fmt.Errorf("no WinPE boot.wim on the control plane; upload a Windows Server ISO")
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	return os.Rename(tmp, dest)
}

func (s *Server) serveWimboot(w http.ResponseWriter, r *http.Request) {
	b, err := bootstrap.ReadWimboot()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(b)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}

func (s *Server) serveWinPECurl(w http.ResponseWriter, r *http.Request) {
	path := winpecurl.Path(s.Cfg)
	if path == "" {
		http.Error(w, "winpe-curl.exe missing; copy it from the Release tarball next to rackauto", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", st.Size()))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, "curl.exe", st.ModTime(), f)
}

func (s *Server) imagesHTTP() http.Handler {
	fs := http.StripPrefix("/images/", http.FileServer(http.Dir(s.Cfg.ImagesDir())))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/images/"), "/")
		parts := strings.Split(p, "/")
		if r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "win" && parts[1] != "" && parts[2] != "" {
			r.SetPathValue("id", parts[1])
			r.SetPathValue("name", parts[2])
			s.serveWinPayload(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	})
}

func (s *Server) serveWinPayload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := strings.ToLower(filepath.Base(r.PathValue("name")))
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		http.NotFound(w, r)
		return
	}
	img, err := s.Store.GetImage(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch name {
	case "boot.wim":
		p := filepath.Join(s.Cfg.ImagesDir(), "win", id, "boot.wim")
		if _, err := os.Stat(p); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, p)
	case "install.wim", "install.esd":
		extracted := filepath.Join(s.Cfg.ImagesDir(), "win", id, name)
		want := int64(0)
		if img.Inspect != nil {
			want = img.Inspect.InstallSize
		}
		if st, err := os.Stat(extracted); err == nil && st.Size() > 0 && (want <= 0 || st.Size() >= want) {
			http.ServeFile(w, r, extracted)
			return
		}
		src := ""
		off, sz := int64(0), int64(0)
		if img.Inspect != nil {
			off, sz = img.Inspect.InstallOff, img.Inspect.InstallSize
			if img.Inspect.InstallFrom != "" {
				src = filepath.Join(s.Cfg.ImagesDir(), img.Inspect.InstallFrom)
			}
		}
		if src == "" {
			if p, err := s.imageFile(img); err == nil {
				src = p
			}
		}
		if src == "" {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(src)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		if st, err := f.Stat(); err == nil {
			if n, err := imageinspect.WIMLengthAt(f, off, st.Size()); err == nil && n > sz {
				sz = n
			}
		}
		if sz <= 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", sz))
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Cache-Control", "public, max-age=60")
		if r.Method == http.MethodHead {
			return
		}
		if _, err := io.CopyN(w, io.NewSectionReader(f, off, sz), sz); err != nil {
			log.Printf("serve %s: %v", name, err)
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) windowsJobForMAC(mac string) (model.Job, model.Image, model.InstallSpec, bool) {
	mac = netboot.NormalizeMAC(mac)
	if s.Netboot == nil || s.Netboot.Store == nil {
		return model.Job{}, model.Image{}, model.InstallSpec{}, false
	}
	return s.Netboot.Store.WindowsInstall(mac)
}

func (s *Server) serveWindowsIpxeFile(w http.ResponseWriter, r *http.Request) {
	mac := netboot.NormalizeMAC(r.PathValue("mac"))
	name := strings.ToLower(r.PathValue("name"))
	job, img, spec, ok := s.windowsJobForMAC(mac)
	if !ok {
		http.NotFound(w, r)
		return
	}
	token := s.token()
	base := httpBase(r, s.Netboot.PublicURL())
	media := provision.WindowsJobMedia(base, token, job.ID, mac, spec, img)
	var body string
	ctype := "text/plain; charset=utf-8"
	switch name {
	case "startnet.cmd":
		body = media.Startnet
	case "winpeshl.ini":
		body = media.Winpeshl
	case "diskpart.txt":
		body = media.Diskpart
	case "install.cmd":
		body = media.Install
	case "unattend.xml":
		body = media.Unattend
		ctype = "application/xml; charset=utf-8"
	case "complete.json":
		body = media.CompleteJSON
		ctype = "application/json"
	case "fail.json":
		body = media.FailJSON
		ctype = "application/json"
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

func (s *Server) removeWindowsPayload(id string) {
	if id == "" || strings.Contains(id, "..") {
		return
	}
	p := filepath.Join(s.Cfg.ImagesDir(), "win", id)
	if err := os.RemoveAll(p); err != nil {
		log.Printf("remove windows payload %s: %v", id, err)
	}
}

func windowsBootWIMExists(cfgImagesDir, imageID string, img model.Image) bool {
	candidates := []string{
		filepath.Join(cfgImagesDir, "win", imageID, "boot.wim"),
	}
	if img.Inspect != nil && img.Inspect.BootWIM != "" {
		candidates = append(candidates, filepath.Join(cfgImagesDir, img.Inspect.BootWIM))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return true
		}
	}
	return false
}
