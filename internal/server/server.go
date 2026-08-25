package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/bmc"
	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/provision"
	"github.com/Songxwn/Rack-auto/internal/store"
	"github.com/Songxwn/Rack-auto/web"
)

type Server struct {
	Cfg     config.Config
	Store   *store.Store
	Netboot *netboot.Service
}

func New(cfg config.Config, st *store.Store, nb *netboot.Service) *Server {
	return &Server{Cfg: cfg, Store: st, Netboot: nb}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/overview", s.auth(s.overview))
	mux.HandleFunc("GET /api/v1/events", s.auth(s.events))
	mux.HandleFunc("GET /api/v1/settings", s.auth(s.getSettings))
	mux.HandleFunc("PUT /api/v1/settings", s.auth(s.putSettings))
	mux.HandleFunc("GET /api/v1/nics", s.auth(s.listNICs))
	mux.HandleFunc("POST /api/v1/dhcp/apply", s.auth(s.applyDHCP))
	mux.HandleFunc("POST /api/v1/dhcp/stop", s.auth(s.stopDHCP))

	mux.HandleFunc("GET /api/v1/machines", s.auth(s.listMachines))
	mux.HandleFunc("POST /api/v1/machines", s.auth(s.createMachine))
	mux.HandleFunc("GET /api/v1/machines/{id}", s.auth(s.getMachine))
	mux.HandleFunc("PUT /api/v1/machines/{id}", s.auth(s.updateMachine))
	mux.HandleFunc("DELETE /api/v1/machines/{id}", s.auth(s.deleteMachine))
	mux.HandleFunc("POST /api/v1/machines/{id}/power", s.auth(s.power))
	mux.HandleFunc("GET /api/v1/machines/{id}/power", s.auth(s.powerStatus))
	mux.HandleFunc("POST /api/v1/machines/{id}/boot", s.auth(s.setBoot))
	mux.HandleFunc("POST /api/v1/machines/{id}/pxe-install", s.auth(s.pxeInstall))

	mux.HandleFunc("GET /api/v1/images", s.auth(s.listImages))
	mux.HandleFunc("POST /api/v1/images", s.auth(s.createImage))
	mux.HandleFunc("POST /api/v1/images/upload", s.auth(s.uploadImage))
	mux.HandleFunc("DELETE /api/v1/images/{id}", s.auth(s.deleteImage))
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(s.Cfg.ImagesDir()))))

	mux.HandleFunc("GET /api/v1/jobs", s.auth(s.listJobs))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.auth(s.getJob))
	mux.HandleFunc("POST /api/v1/jobs/install", s.auth(s.createInstall))
	mux.HandleFunc("POST /api/v1/jobs/stress", s.auth(s.createStress))
	mux.HandleFunc("POST /api/v1/jobs/{id}/cancel", s.auth(s.cancelJob))

	mux.HandleFunc("POST /api/v1/agent/register", s.auth(s.agentRegister))
	mux.HandleFunc("POST /api/v1/agent/heartbeat", s.auth(s.agentHeartbeat))
	mux.HandleFunc("GET /api/v1/agent/job", s.auth(s.agentJob))
	mux.HandleFunc("POST /api/v1/agent/jobs/{id}/log", s.auth(s.agentLog))
	mux.HandleFunc("POST /api/v1/agent/jobs/{id}/progress", s.auth(s.agentProgress))
	mux.HandleFunc("POST /api/v1/agent/jobs/{id}/complete", s.auth(s.agentComplete))
	mux.HandleFunc("GET /api/v1/agent/speedtest", s.auth(s.speedDownload))
	mux.HandleFunc("POST /api/v1/agent/speedtest", s.auth(s.speedUpload))

	mux.HandleFunc("GET /ipxe/boot.ipxe", s.ipxeMenu)
	mux.HandleFunc("GET /ipxe/script", s.ipxeScript)
	mux.HandleFunc("GET /ipxe/apkovl.tgz", s.apkovl)

	mux.Handle("/boot/agent/", http.StripPrefix("/boot/agent/", http.FileServer(http.Dir(s.Cfg.AgentDir()))))
	mux.Handle("/ramos/", http.StripPrefix("/ramos/", http.FileServer(http.Dir(s.Cfg.RAMOSDir()))))
	mux.Handle("/tftp/", http.StripPrefix("/tftp/", http.FileServer(http.Dir(s.Cfg.TFTPDir()))))

	sub, _ := fs.Sub(web.FS, ".")
	fileSrv := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ipxe/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(sub, name); err == nil {
			fileSrv.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileSrv.ServeHTTP(w, r)
	})
	return withLog(mux)
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ipxe/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Truncate(time.Millisecond))
		}
	})
}

func (s *Server) token() string {
	return s.Store.Setting("api_token", s.Cfg.APIToken)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := s.token()
		if tok == "" {
			next(w, r)
			return
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
		if got != tok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "name": "rackauto"})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	ov, err := s.Store.Overview()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	st := s.Netboot.DHCPStatus()
	ov.DHCPRunning = st.Running
	ov.DHCPInterface = st.Interface
	writeJSON(w, 200, ov)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListEvents(80)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	nics, err := netboot.ListNICs()
	if err != nil {
		nics = []netboot.HostNIC{}
	}
	writeJSON(w, 200, map[string]any{
		"public_url":    s.Store.Setting("public_url", s.Cfg.PublicURL),
		"api_token_set": s.token() != "",
		"tftp_listen":   s.Cfg.TFTPListen,
		"listen":        s.Cfg.Listen,
		"data_dir":      s.Cfg.DataDir,
		"dhcp":          s.Netboot.CurrentDHCP(),
		"dhcp_status":   s.Netboot.DHCPStatus(),
		"nics":          nics,
	})
}

func (s *Server) listNICs(w http.ResponseWriter, r *http.Request) {
	nics, err := netboot.ListNICs()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, nics)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PublicURL string       `json:"public_url"`
		APIToken  string       `json:"api_token"`
		DHCP      *config.DHCP `json:"dhcp"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if in.PublicURL != "" {
		_ = s.Store.SetSetting("public_url", strings.TrimRight(in.PublicURL, "/"))
	}
	if in.APIToken != "" {
		_ = s.Store.SetSetting("api_token", in.APIToken)
	}
	if in.DHCP != nil {
		if err := s.Netboot.ApplyDHCP(*in.DHCP); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		s.Store.AddEvent("info", dhcpEvent(*in.DHCP, s.Netboot.DHCPStatus().Running), "")
	}
	s.getSettings(w, r)
}

func (s *Server) applyDHCP(w http.ResponseWriter, r *http.Request) {
	var d config.DHCP
	if err := readJSON(r, &d); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.Netboot.ApplyDHCP(d); err != nil {
		s.Store.AddEvent("error", "DHCP 应用失败: "+err.Error(), "")
		http.Error(w, err.Error(), 400)
		return
	}
	s.Store.AddEvent("info", dhcpEvent(d, s.Netboot.DHCPStatus().Running), "")
	s.getSettings(w, r)
}

func (s *Server) stopDHCP(w http.ResponseWriter, r *http.Request) {
	s.Netboot.StopDHCP()
	s.Store.AddEvent("info", "已停止内置 DHCP", "")
	s.getSettings(w, r)
}

func dhcpEvent(d config.DHCP, running bool) string {
	if !d.Enabled {
		return "已关闭内置 DHCP"
	}
	if running {
		return "DHCP 已在接入网卡 " + d.Interface + " 上运行"
	}
	return "已保存 DHCP 配置（接入网卡 " + d.Interface + "）"
}

func (s *Server) listMachines(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListMachines()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	out := make([]model.Machine, 0, len(list))
	for _, m := range list {
		out = append(out, m.Public())
	}
	writeJSON(w, 200, out)
}

func (s *Server) createMachine(w http.ResponseWriter, r *http.Request) {
	var m model.Machine
	if err := readJSON(r, &m); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	m.MAC = netboot.NormalizeMAC(m.MAC)
	if m.MAC == "" {
		http.Error(w, "mac required", 400)
		return
	}
	if m.Name == "" {
		m.Name = m.MAC
	}
	if m.Status == "" {
		m.Status = model.MachineReady
	}
	if m.BootMode == "" {
		m.BootMode = model.BootRAM
	}
	if m.BMCPort == 0 && m.BMCType != model.BMCRedfish {
		m.BMCPort = 623
	}
	if err := s.Store.UpsertMachine(&m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.Store.AddEvent("info", "登记机器 "+m.Name+" ("+m.MAC+")", m.ID)
	writeJSON(w, 201, m.Public())
}

func (s *Server) getMachine(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.GetMachine(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, 200, m.Public())
}

func (s *Server) updateMachine(w http.ResponseWriter, r *http.Request) {
	cur, err := s.Store.GetMachine(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	var in model.Machine
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	in.ID = cur.ID
	in.CreatedAt = cur.CreatedAt
	if in.MAC == "" {
		in.MAC = cur.MAC
	} else {
		in.MAC = netboot.NormalizeMAC(in.MAC)
	}
	if in.BMCPassword == "" {
		in.BMCPassword = cur.BMCPassword
	}
	if in.Inventory == nil {
		in.Inventory = cur.Inventory
	}
	if err := s.Store.UpsertMachine(&in); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, in.Public())
}

func (s *Server) deleteMachine(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteMachine(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) withBMC(w http.ResponseWriter, r *http.Request, fn func(bmc.Controller, model.Machine) error) {
	m, err := s.Store.GetMachine(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	ctl, err := bmc.Open(m)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := fn(ctl, m); err != nil {
		s.Store.AddEvent("error", err.Error(), m.ID)
		http.Error(w, err.Error(), 502)
		return
	}
}

func (s *Server) power(w http.ResponseWriter, r *http.Request) {
	var req model.PowerRequest
	if err := readJSON(r, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.withBMC(w, r, func(ctl bmc.Controller, m model.Machine) error {
		ctx := r.Context()
		if err := ctl.Power(ctx, req.Action); err != nil {
			return err
		}
		s.Store.AddEvent("info", "BMC 电源 "+req.Action+" → "+m.Name, m.ID)
		writeJSON(w, 200, map[string]any{"ok": true, "action": req.Action})
		return nil
	})
}

func (s *Server) powerStatus(w http.ResponseWriter, r *http.Request) {
	s.withBMC(w, r, func(ctl bmc.Controller, m model.Machine) error {
		st, err := ctl.PowerStatus(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, 200, map[string]any{"status": st})
		return nil
	})
}

func (s *Server) setBoot(w http.ResponseWriter, r *http.Request) {
	var req model.BootRequest
	if err := readJSON(r, &req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.withBMC(w, r, func(ctl bmc.Controller, m model.Machine) error {
		fw := req.Firmware
		if fw == "" {
			fw = m.Firmware
		}
		if err := ctl.SetBoot(r.Context(), req.Device, fw, req.Persistent); err != nil {
			return err
		}
		mode := model.BootPXE
		if req.Device == "disk" || req.Device == "hdd" {
			mode = model.BootDisk
		}
		_ = s.Store.SetBootMode(m.ID, mode)
		s.Store.AddEvent("info", "BMC 引导 "+req.Device+"/"+fw+" → "+m.Name, m.ID)
		writeJSON(w, 200, map[string]any{"ok": true})
		return nil
	})
}

func (s *Server) pxeInstall(w http.ResponseWriter, r *http.Request) {
	s.withBMC(w, r, func(ctl bmc.Controller, m model.Machine) error {
		fw := m.Firmware
		if fw == "" {
			fw = model.FirmwareUEFI
		}
		if err := ctl.SetBoot(r.Context(), "pxe", fw, false); err != nil {
			return err
		}
		_ = s.Store.SetBootMode(m.ID, model.BootRAM)
		if err := ctl.Power(r.Context(), "cycle"); err != nil {
			_ = ctl.Power(r.Context(), "on")
		}
		s.Store.AddEvent("info", "已设置 PXE 并重启 "+m.Name, m.ID)
		writeJSON(w, 200, map[string]any{"ok": true})
		return nil
	})
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListImages()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) createImage(w http.ResponseWriter, r *http.Request) {
	var img model.Image
	if err := readJSON(r, &img); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if img.Name == "" || img.URL == "" {
		http.Error(w, "name and url required", 400)
		return
	}
	if img.Kind == "" {
		img.Kind = model.ImageCloudDisk
	}
	if err := s.Store.UpsertImage(&img); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.Store.AddEvent("info", "登记镜像 "+img.Name, "")
	writeJSON(w, 201, img)
}

func (s *Server) uploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<30)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer f.Close()
	name := r.FormValue("name")
	if name == "" {
		name = hdr.Filename
	}
	dstName := fmt.Sprintf("%d-%s", time.Now().Unix(), filepath.Base(hdr.Filename))
	dst := filepath.Join(s.Cfg.ImagesDir(), dstName)
	out, err := os.Create(dst)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	n, err := io.Copy(out, f)
	_ = out.Close()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	img := model.Image{
		Name:     name,
		OSFamily: r.FormValue("os_family"),
		Kind:     r.FormValue("kind"),
		URL:      s.Store.Setting("public_url", s.Cfg.PublicURL) + "/images/" + dstName,
		Filename: dstName,
		SizeB:    n,
	}
	if img.Kind == "" {
		img.Kind = model.ImageCloudDisk
	}
	if err := s.Store.UpsertImage(&img); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 201, img)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	img, err := s.Store.GetImage(r.PathValue("id"))
	if err == nil && img.Filename != "" {
		_ = os.Remove(filepath.Join(s.Cfg.ImagesDir(), img.Filename))
	}
	if err := s.Store.DeleteImage(r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListJobs(r.URL.Query().Get("machine_id"), 200)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	out := make([]model.Job, 0, len(list))
	for _, j := range list {
		j.Logs = ""
		out = append(out, store.RedactJob(j))
	}
	writeJSON(w, 200, out)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.Store.GetJob(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, 200, store.RedactJob(j))
}

func (s *Server) createInstall(w http.ResponseWriter, r *http.Request) {
	var spec struct {
		MachineID string `json:"machine_id"`
		model.InstallSpec
	}
	if err := readJSON(r, &spec); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if spec.MachineID == "" || spec.ImageID == "" {
		http.Error(w, "machine_id and image_id required", 400)
		return
	}
	m, err := s.Store.GetMachine(spec.MachineID)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if _, err := s.Store.GetImage(spec.ImageID); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if len(spec.Partitions) == 0 {
		fw := spec.Firmware
		if fw == "" {
			fw = m.Firmware
		}
		spec.Partitions = provision.DefaultPartitions(fw)
	}
	if err := provision.Validate(spec.Partitions); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if spec.Hostname == "" {
		spec.Hostname = m.Name
	}
	job := model.Job{
		Type:      model.JobInstall,
		MachineID: m.ID,
		ImageID:   spec.ImageID,
		Status:    model.JobPending,
		Params:    spec.InstallSpec,
		Message:   "等待 RAMOS Agent 领取任务",
	}
	if err := s.Store.InsertJob(&job); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_ = s.Store.SetMachineStatus(m.ID, model.MachineInstalling)
	_ = s.Store.SetBootMode(m.ID, model.BootRAM)
	s.Store.AddEvent("info", "创建装机任务 "+job.ID+" → "+m.Name, m.ID)
	writeJSON(w, 201, store.RedactJob(job))
}

func (s *Server) createStress(w http.ResponseWriter, r *http.Request) {
	var spec struct {
		MachineID string `json:"machine_id"`
		model.StressSpec
	}
	if err := readJSON(r, &spec); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if spec.MachineID == "" {
		http.Error(w, "machine_id required", 400)
		return
	}
	if spec.DurationSec <= 0 {
		spec.DurationSec = 60
	}
	if len(spec.Targets) == 0 {
		spec.Targets = []string{"cpu", "memory", "disk", "network"}
	}
	m, err := s.Store.GetMachine(spec.MachineID)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	job := model.Job{
		Type:      model.JobStress,
		MachineID: m.ID,
		Status:    model.JobPending,
		Params:    spec.StressSpec,
		Message:   "等待 Agent 领取压测任务",
	}
	if err := s.Store.InsertJob(&job); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_ = s.Store.SetMachineStatus(m.ID, model.MachineStressing)
	s.Store.AddEvent("info", "创建压测任务 "+job.ID+" → "+m.Name, m.ID)
	writeJSON(w, 201, job)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.Store.GetJob(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if j.Status == model.JobSucceeded || j.Status == model.JobFailed {
		http.Error(w, "job already finished", 400)
		return
	}
	j.Status = model.JobCancelled
	t := time.Now().UTC()
	j.FinishedAt = &t
	j.Message = "已取消"
	_ = s.Store.UpdateJob(j)
	writeJSON(w, 200, store.RedactJob(j))
}

func (s *Server) agentRegister(w http.ResponseWriter, r *http.Request) {
	var in model.AgentRegister
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	in.MAC = netboot.NormalizeMAC(in.MAC)
	if in.MAC == "" {
		http.Error(w, "mac required", 400)
		return
	}
	m, err := s.Store.GetMachineByMAC(in.MAC)
	now := time.Now().UTC()
	if err != nil {
		fw := model.FirmwareBIOS
		if in.Inventory != nil && in.Inventory.Firmware != "" {
			fw = in.Inventory.Firmware
		}
		name := in.Hostname
		if name == "" {
			name = in.MAC
		}
		m = model.Machine{
			Name: name, MAC: in.MAC, IP: in.IP, Status: model.MachineDiscovered,
			Firmware: fw, BootMode: model.BootRAM, Inventory: in.Inventory,
			AgentVersion: in.AgentVersion, LastSeen: &now,
		}
		if err := s.Store.UpsertMachine(&m); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.Store.AddEvent("info", "发现新机器 "+m.Name+" "+m.MAC, m.ID)
	} else {
		fw := m.Firmware
		if in.Inventory != nil && in.Inventory.Firmware != "" {
			fw = in.Inventory.Firmware
		}
		st := m.Status
		if st == model.MachineOffline || st == "" {
			st = model.MachineReady
		}
		_ = s.Store.TouchMachine(m.ID, in.IP, st, fw, in.AgentVersion, in.Inventory)
		m, _ = s.Store.GetMachine(m.ID)
	}
	writeJSON(w, 200, map[string]any{"machine_id": m.ID, "name": m.Name})
}

func (s *Server) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	s.agentRegister(w, r)
}

func (s *Server) agentJob(w http.ResponseWriter, r *http.Request) {
	mac := netboot.NormalizeMAC(r.URL.Query().Get("mac"))
	id := r.URL.Query().Get("machine_id")
	var m model.Machine
	var err error
	if id != "" {
		m, err = s.Store.GetMachine(id)
	} else {
		m, err = s.Store.GetMachineByMAC(mac)
	}
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	j, err := s.Store.NextPendingJob(m.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"job": nil})
		return
	}
	now := time.Now().UTC()
	j.Status = model.JobRunning
	j.StartedAt = &now
	j.Message = "Agent 已领取"
	_ = s.Store.UpdateJob(j)
	aj := model.AgentJob{ID: j.ID, Type: j.Type, Params: j.Params}
	if j.ImageID != "" {
		if img, err := s.Store.GetImage(j.ImageID); err == nil {
			aj.Image = &img
		}
	}
	writeJSON(w, 200, map[string]any{"job": aj})
}

func (s *Server) agentLog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Line string `json:"line"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	_ = s.Store.AppendJobLog(r.PathValue("id"), in.Line)
	w.WriteHeader(204)
}

func (s *Server) agentProgress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Progress int    `json:"progress"`
		Message  string `json:"message"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	j, err := s.Store.GetJob(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	j.Progress = in.Progress
	if in.Message != "" {
		j.Message = in.Message
	}
	_ = s.Store.UpdateJob(j)
	w.WriteHeader(204)
}

func (s *Server) agentComplete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Result  any    `json:"result"`
	}
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	j, err := s.Store.GetJob(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	now := time.Now().UTC()
	j.FinishedAt = &now
	j.Result = in.Result
	j.Message = in.Message
	if in.OK {
		j.Status = model.JobSucceeded
		j.Progress = 100
		if j.Type == model.JobInstall {
			_ = s.Store.SetMachineStatus(j.MachineID, model.MachineProvisioned)
			_ = s.Store.SetBootMode(j.MachineID, model.BootDisk)
		} else {
			_ = s.Store.SetMachineStatus(j.MachineID, model.MachineReady)
		}
	} else {
		j.Status = model.JobFailed
		_ = s.Store.SetMachineStatus(j.MachineID, model.MachineError)
	}
	_ = s.Store.UpdateJob(j)
	level := "info"
	if !in.OK {
		level = "error"
	}
	s.Store.AddEvent(level, "任务 "+j.ID+" "+j.Status+" "+in.Message, j.MachineID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) speedDownload(w http.ResponseWriter, r *http.Request) {
	mb, _ := strconv.Atoi(r.URL.Query().Get("mb"))
	if mb <= 0 {
		mb = 64
	}
	if mb > 1024 {
		mb = 1024
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(mb<<20))
	buf := make([]byte, 1<<20)
	_, _ = rand.Read(buf[:4096])
	for i := 0; i < mb; i++ {
		if _, err := w.Write(buf); err != nil {
			return
		}
	}
}

func (s *Server) speedUpload(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, io.LimitReader(r.Body, 2<<30))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, 200, map[string]any{"bytes": n})
}

func (s *Server) ipxeMenu(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(s.Netboot.MenuScript()))
}

func (s *Server) ipxeScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(s.Netboot.ScriptFor(r.URL.Query().Get("mac"), r.URL.Query().Get("arch"), r.URL.Query().Get("platform"))))
}

func (s *Server) apkovl(w http.ResponseWriter, r *http.Request) {
	b, err := s.Netboot.APKOVL(r.URL.Query().Get("mac"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(b)
}

func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
