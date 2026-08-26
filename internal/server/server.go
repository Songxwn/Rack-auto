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
	"sync"
	"time"

	"github.com/Songxwn/Rack-auto/internal/bmc"
	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/imageinspect"
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/osprofile"
	"github.com/Songxwn/Rack-auto/internal/provision"
	"github.com/Songxwn/Rack-auto/internal/store"
	"github.com/Songxwn/Rack-auto/web"
)

type Server struct {
	Cfg     config.Config
	Store   *store.Store
	Netboot *netboot.Service
	Version string

	sessMu     sync.Mutex
	sessions   map[string]webSession
	loginMu    sync.Mutex
	loginFails map[string]loginGuard
}

func New(cfg config.Config, st *store.Store, nb *netboot.Service) *Server {
	s := &Server{
		Cfg:        cfg,
		Store:      st,
		Netboot:    nb,
		sessions:   map[string]webSession{},
		loginFails: map[string]loginGuard{},
	}
	if err := s.ensureWebAccount(); err != nil {
		log.Printf("web account: %v", err)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/login", s.login)
	mux.HandleFunc("POST /api/v1/logout", s.logout)
	mux.HandleFunc("GET /api/v1/session", s.getSession)
	mux.HandleFunc("PUT /api/v1/account", s.webAuth(s.putAccount))

	mux.HandleFunc("GET /api/v1/overview", s.webAuth(s.overview))
	mux.HandleFunc("GET /api/v1/events", s.webAuth(s.events))
	mux.HandleFunc("GET /api/v1/settings", s.webAuth(s.getSettings))
	mux.HandleFunc("PUT /api/v1/settings", s.webAuth(s.putSettings))
	mux.HandleFunc("GET /api/v1/nics", s.webAuth(s.listNICs))
	mux.HandleFunc("POST /api/v1/dhcp/apply", s.webAuth(s.applyDHCP))
	mux.HandleFunc("POST /api/v1/dhcp/stop", s.webAuth(s.stopDHCP))

	mux.HandleFunc("GET /api/v1/machines", s.webAuth(s.listMachines))
	mux.HandleFunc("POST /api/v1/machines", s.webAuth(s.createMachine))
	mux.HandleFunc("GET /api/v1/machines/{id}", s.webAuth(s.getMachine))
	mux.HandleFunc("PUT /api/v1/machines/{id}", s.webAuth(s.updateMachine))
	mux.HandleFunc("DELETE /api/v1/machines/{id}", s.webAuth(s.deleteMachine))
	mux.HandleFunc("POST /api/v1/machines/{id}/power", s.webAuth(s.power))
	mux.HandleFunc("GET /api/v1/machines/{id}/power", s.webAuth(s.powerStatus))
	mux.HandleFunc("POST /api/v1/machines/{id}/boot", s.webAuth(s.setBoot))
	mux.HandleFunc("POST /api/v1/machines/{id}/pxe-install", s.webAuth(s.pxeInstall))
	mux.HandleFunc("POST /api/v1/machines/{id}/detect", s.webAuth(s.detectMachine))

	mux.HandleFunc("GET /api/v1/os-catalog", s.webAuth(s.osCatalog))
	mux.HandleFunc("GET /api/v1/windows/kms-keys", s.webAuth(s.windowsKMSKeys))
	mux.HandleFunc("GET /api/v1/images", s.webAuth(s.listImages))
	mux.HandleFunc("POST /api/v1/images", s.webAuth(s.createImage))
	mux.HandleFunc("POST /api/v1/images/upload", s.webAuth(s.uploadImage))
	mux.HandleFunc("POST /api/v1/images/{id}/inspect", s.webAuth(s.inspectImage))
	mux.HandleFunc("DELETE /api/v1/images/{id}", s.webAuth(s.deleteImage))
	mux.Handle("/images/", s.imagesHTTP())

	mux.HandleFunc("GET /api/v1/templates", s.webAuth(s.listTemplates))
	mux.HandleFunc("POST /api/v1/templates", s.webAuth(s.createTemplate))
	mux.HandleFunc("GET /api/v1/templates/{id}", s.webAuth(s.getTemplate))
	mux.HandleFunc("PUT /api/v1/templates/{id}", s.webAuth(s.updateTemplate))
	mux.HandleFunc("DELETE /api/v1/templates/{id}", s.webAuth(s.deleteTemplate))

	mux.HandleFunc("GET /api/v1/jobs", s.webAuth(s.listJobs))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.webAuth(s.getJob))
	mux.HandleFunc("DELETE /api/v1/jobs/{id}", s.webAuth(s.deleteJob))
	mux.HandleFunc("POST /api/v1/jobs/install", s.webAuth(s.createInstall))
	mux.HandleFunc("POST /api/v1/jobs/stress", s.webAuth(s.createStress))
	mux.HandleFunc("POST /api/v1/jobs/{id}/cancel", s.webAuth(s.cancelJob))

	mux.HandleFunc("POST /api/v1/agent/register", s.auth(s.agentRegister))
	mux.HandleFunc("POST /api/v1/agent/heartbeat", s.auth(s.agentHeartbeat))
	mux.HandleFunc("GET /api/v1/agent/job", s.auth(s.agentJob))
	mux.HandleFunc("POST /api/v1/agent/jobs/{id}/log", s.auth(s.agentLog))
	mux.HandleFunc("POST /api/v1/agent/jobs/{id}/progress", s.auth(s.agentProgress))
	mux.HandleFunc("GET /api/v1/agent/jobs/{id}/progress", s.auth(s.agentProgress))
	mux.HandleFunc("POST /api/v1/agent/jobs/{id}/complete", s.auth(s.agentComplete))
	mux.HandleFunc("GET /api/v1/agent/jobs/{id}/complete", s.auth(s.agentComplete))
	mux.HandleFunc("GET /api/v1/agent/speedtest", s.auth(s.speedDownload))
	mux.HandleFunc("POST /api/v1/agent/speedtest", s.auth(s.speedUpload))

	mux.HandleFunc("GET /ipxe/boot.ipxe", s.ipxeMenu)
	mux.HandleFunc("GET /ipxe/script", s.ipxeScript)
	mux.HandleFunc("GET /ipxe/ramos-start.sh", s.ramosStart)
	mux.HandleFunc("GET /ipxe/cidata/{mac}/user-data", s.cidataUserData)
	mux.HandleFunc("GET /ipxe/cidata/{mac}/meta-data", s.cidataMetaData)
	mux.HandleFunc("GET /ipxe/cidata/{mac}/vendor-data", s.cidataVendorData)
	mux.HandleFunc("GET /ipxe/windows/{mac}/{name}", s.serveWindowsIpxeFile)
	mux.HandleFunc("GET /winpe/wimboot", s.serveWimboot)
	mux.HandleFunc("GET /winpe/curl.exe", s.serveWinPECurl)

	mux.Handle("/boot/agent/", http.StripPrefix("/boot/agent/", http.FileServer(http.Dir(s.Cfg.AgentDir()))))
	mux.Handle("/ramos/", http.StripPrefix("/ramos/", http.FileServer(http.Dir(s.Cfg.RAMOSDir()))))
	mux.Handle("/tftp/", http.StripPrefix("/tftp/", http.FileServer(http.Dir(s.Cfg.TFTPDir()))))

	sub, _ := fs.Sub(web.FS, ".")
	fileSrv := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ipxe/") || strings.HasPrefix(r.URL.Path, "/winpe/") {
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
		if filepath.Ext(name) != "" {
			http.NotFound(w, r)
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
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ipxe/") || strings.HasPrefix(r.URL.Path, "/winpe/") {
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
	ver := s.Version
	if ver == "" {
		ver = "dev"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": "rackauto", "version": ver})
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
	id := r.PathValue("id")
	m, err := s.Store.GetMachine(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if err := s.Store.DeleteMachine(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	name := m.Name
	if name == "" {
		name = m.MAC
	}
	if name == "" {
		name = id
	}
	s.Store.AddEvent("info", "删除机器 "+name, id)
	w.WriteHeader(204)
}

func (s *Server) detectMachine(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.GetMachine(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	inv := m.Inventory
	if inv == nil {
		inv = &model.Inventory{}
	}
	if strings.EqualFold(m.BMCType, model.BMCRedfish) && strings.TrimSpace(m.BMCAddress) != "" {
		ctl, err := bmc.Open(m)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		rf, ok := ctl.(*bmc.Redfish)
		if !ok {
			http.Error(w, "BMC is not Redfish", 400)
			return
		}
		got, err := rf.ReadInventory(r.Context())
		if err != nil {
			s.Store.AddEvent("error", "detect hardware failed: "+err.Error(), m.ID)
			http.Error(w, err.Error(), 502)
			return
		}
		inv = bmc.MergeIdentity(inv, got)
	}
	if !inv.HasIdentity() && inv.BIOSVersion == "" {
		http.Error(w, "还没有品牌/型号/序列号。请 PXE 进入 RAMOS 让 Agent 读取 DMI，或配置 Redfish 后再检测。", 409)
		return
	}
	m.Inventory = inv
	if next := autoMachineName(m.Name, m.MAC, inv); next != m.Name {
		m.Name = next
	}
	if err := s.Store.UpsertMachine(&m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	line := inv.ProductLine()
	if line == "" {
		line = m.Name
	}
	msg := "检测硬件 " + line
	if inv.Serial != "" {
		msg += " SN " + inv.Serial
	}
	s.Store.AddEvent("info", msg, m.ID)
	writeJSON(w, 200, m.Public())
}

func autoMachineName(cur, mac string, inv *model.Inventory) string {
	ident := ""
	if inv != nil {
		ident = inv.IdentityName()
	}
	if ident != "" {
		if genericMachineName(cur, mac) {
			return ident
		}
		n := strings.TrimSpace(cur)
		if inv != nil && (strings.EqualFold(n, inv.ModelName()) || strings.EqualFold(n, inv.ProductLine())) {
			return ident
		}
		if inv != nil && inv.Serial != "" && strings.EqualFold(n, "SN "+inv.Serial) {
			return ident
		}
		return cur
	}
	if model.IsLiveHostname(cur) {
		if mac = netboot.NormalizeMAC(mac); mac != "" {
			return mac
		}
	}
	return cur
}

func genericMachineName(name, mac string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	mac = netboot.NormalizeMAC(mac)
	compact := strings.ReplaceAll(mac, ":", "")
	if n == "" || n == mac || strings.ReplaceAll(n, ":", "") == compact {
		return true
	}
	return model.IsLiveHostname(name)
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

func (s *Server) osCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, osprofile.Catalog())
}

func (s *Server) windowsKMSKeys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, provision.ListKMSKeys())
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListImages()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for i := range list {
		if list[i].Inspect != nil {
			continue
		}
		s.fillImageInspect(&list[i])
		_ = s.Store.UpsertImage(&list[i])
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
		OSFamily:  r.FormValue("os_family"),
		OSVersion: r.FormValue("os_version"),
		Kind:      r.FormValue("kind"),
		URL:      s.Store.Setting("public_url", s.Cfg.PublicURL) + "/images/" + dstName,
		Filename: dstName,
		SizeB:    n,
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
	writeJSON(w, 201, img)
}

func (s *Server) inspectImage(w http.ResponseWriter, r *http.Request) {
	img, err := s.Store.GetImage(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	s.fillImageInspect(&img)
	if err := s.Store.UpsertImage(&img); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, img)
}

func (s *Server) fillImageInspect(img *model.Image) {
	path, err := s.imageFile(*img)
	if err != nil {
		img.Inspect = &model.ImageInspect{
			Status:      "skipped",
			Message:     "file is not on this control plane; upload it to inspect bootloaders",
			InspectedAt: time.Now().UTC(),
		}
		return
	}
	if img.SizeB == 0 {
		if st, err := os.Stat(path); err == nil {
			img.SizeB = st.Size()
		}
	}
	img.Inspect = imageinspect.File(path)
	s.coerceWindowsImage(img)
	if img.Inspect != nil && img.Inspect.Windows {
		s.materializeWindows(img, path)
	}
}

func (s *Server) imageFile(img model.Image) (string, error) {
	if img.Filename != "" {
		p := filepath.Join(s.Cfg.ImagesDir(), img.Filename)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p, nil
		}
	}
	u := img.URL
	const mark = "/images/"
	if i := strings.LastIndex(u, mark); i >= 0 {
		name := u[i+len(mark):]
		if name != "" && !strings.Contains(name, "/") && !strings.Contains(name, "..") {
			p := filepath.Join(s.Cfg.ImagesDir(), name)
			if st, err := os.Stat(p); err == nil && st.Size() > 0 {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("not local")
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	img, err := s.Store.GetImage(id)
	if err == nil && img.Filename != "" {
		_ = os.Remove(filepath.Join(s.Cfg.ImagesDir(), img.Filename))
	}
	s.removeWindowsPayload(id)
	if err := s.Store.DeleteImage(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

func cleanSSHKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || strings.HasPrefix(k, "#") {
			continue
		}
		out = append(out, k)
	}
	return out
}

func normalizeTemplate(t *model.CredentialTemplate) error {
	t.Name = strings.TrimSpace(t.Name)
	t.Username = strings.TrimSpace(t.Username)
	t.Notes = strings.TrimSpace(t.Notes)
	t.Kind = strings.ToLower(strings.TrimSpace(t.Kind))
	t.SSHKeys = cleanSSHKeys(t.SSHKeys)
	if t.Name == "" {
		return fmt.Errorf("name required")
	}
	switch t.Kind {
	case model.TemplateAccount:
		if t.Username == "" {
			return fmt.Errorf("username required")
		}
	case model.TemplateKey:
		if len(t.SSHKeys) == 0 {
			return fmt.Errorf("ssh_keys required")
		}
		t.Username = ""
		t.Password = ""
	default:
		return fmt.Errorf("kind must be account or key")
	}
	return nil
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListCredTemplates()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	out := make([]model.CredentialTemplate, 0, len(list))
	for _, t := range list {
		if kind != "" && t.Kind != kind {
			continue
		}
		out = append(out, t)
	}
	writeJSON(w, 200, out)
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	var t model.CredentialTemplate
	if err := readJSON(r, &t); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	t.ID = ""
	if err := normalizeTemplate(&t); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.Store.UpsertCredTemplate(&t); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.Store.AddEvent("info", "保存模板 "+t.Name, "")
	writeJSON(w, 201, t)
}

func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.GetCredTemplate(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	cur, err := s.Store.GetCredTemplate(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	var in model.CredentialTemplate
	if err := readJSON(r, &in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	in.ID = cur.ID
	in.CreatedAt = cur.CreatedAt
	if in.Kind == "" {
		in.Kind = cur.Kind
	}
	if in.Password == "" {
		in.Password = cur.Password
	}
	if err := normalizeTemplate(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.Store.UpsertCredTemplate(&in); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, in)
}

func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.Store.GetCredTemplate(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if err := s.Store.DeleteCredTemplate(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	name := t.Name
	if name == "" {
		name = id
	}
	s.Store.AddEvent("info", "删除模板 "+name, "")
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
	img, err := s.Store.GetImage(spec.ImageID)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if img.Inspect == nil || img.Inspect.Status == "" || img.Inspect.Status == "skipped" {
		s.fillImageInspect(&img)
		_ = s.Store.UpsertImage(&img)
	}
	fw := spec.Firmware
	if fw == "" {
		fw = m.Firmware
	}
	if fw == "" {
		fw = model.FirmwareUEFI
	}
	spec.Firmware = fw
	if img.IsWindows() {
		if strings.TrimSpace(spec.Password) == "" {
			http.Error(w, "Windows Server requires a password", 400)
			return
		}
		if err := img.Inspect.Compatible(img.Kind, fw); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if !windowsBootWIMExists(s.Cfg.ImagesDir(), img.ID, img) {
			http.Error(w, "WinPE boot.wim missing; upload a Windows Server ISO (windows-iso) of the same generation", 400)
			return
		}
		spec.Partitions = nil
		spec.SSHKeys = nil
		spec.Hostname = provision.WindowsHostname(spec.Hostname)
		if spec.Hostname == "WIN-HOST" && m.Name != "" {
			spec.Hostname = provision.WindowsHostname(m.Name)
		}
		if spec.Username == "" {
			spec.Username = "Administrator"
		}
		if spec.WIMIndex <= 0 && img.Inspect != nil {
			spec.WIMIndex = provision.DefaultWIMIndex(img.Inspect.WIMImages)
		}
		if spec.WIMIndex <= 0 {
			spec.WIMIndex = 1
		}
		if id := strings.TrimSpace(spec.KMSKeyID); id != "" {
			if _, ok := provision.LookupKMSKey(id); !ok {
				http.Error(w, "unknown kms_key_id", 400)
				return
			}
		}
		spec.EnableRDP = true
		if spec.Timezone == "" {
			spec.Timezone = "Asia/Shanghai"
		}
		for i := range spec.Network.NICs {
			if spec.Network.NICs[i].MAC == "" && m.MAC != "" {
				spec.Network.NICs[i].MAC = m.MAC
			}
		}
		if spec.Hostname == "" {
			spec.Hostname = provision.WindowsHostname(m.Name)
		}
		job := model.Job{
			Type:      model.JobInstall,
			MachineID: m.ID,
			ImageID:   spec.ImageID,
			Status:    model.JobPending,
			Params:    spec.InstallSpec,
			Message:   "等待 PXE 进入 Windows PE",
		}
		if err := s.Store.InsertJob(&job); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = s.Store.SetMachineStatus(m.ID, model.MachineInstalling)
		_ = s.Store.SetBootMode(m.ID, model.BootRAM)
		s.Store.AddEvent("info", "创建 Windows 装机任务 "+job.ID+" → "+m.Name, m.ID)
		writeJSON(w, 201, store.RedactJob(job))
		return
	}
	whole := img.Kind == model.ImageCloudDisk || img.Kind == model.ImageRawDisk
	if err := img.Inspect.Compatible(img.Kind, fw); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if whole {
		spec.Partitions = nil
		if img.Inspect != nil && img.Inspect.VirtualSizeB > 0 && m.Inventory != nil {
			var target int64
			if spec.Disk != "" {
				for _, d := range m.Inventory.Disks {
					if d.Path == spec.Disk {
						target = d.SizeB
						break
					}
				}
			} else {
				for _, d := range m.Inventory.Disks {
					if d.SizeB > target {
						target = d.SizeB
					}
				}
			}
			if target > 0 && img.Inspect.VirtualSizeB > target {
				http.Error(w, fmt.Sprintf("image virtual size %d exceeds target disk %d", img.Inspect.VirtualSizeB, target), 400)
				return
			}
		}
	} else {
		if len(spec.Partitions) == 0 {
			spec.Partitions = provision.DefaultPartitions(fw, img.OSFamily, img.OSVersion)
		}
		if err := provision.Validate(spec.Partitions); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
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
	s.releaseMachineIfIdle(j.MachineID)
	writeJSON(w, 200, store.RedactJob(j))
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, err := s.Store.GetJob(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if err := s.Store.DeleteJob(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.releaseMachineIfIdle(j.MachineID)
	s.Store.AddEvent("info", "删除任务 "+j.Type+" "+id, j.MachineID)
	w.WriteHeader(204)
}

func (s *Server) releaseMachineIfIdle(machineID string) {
	if machineID == "" {
		return
	}
	m, err := s.Store.GetMachine(machineID)
	if err != nil {
		return
	}
	if m.Status != model.MachineInstalling && m.Status != model.MachineStressing {
		return
	}
	active, err := s.Store.HasActiveJob(machineID, "")
	if err != nil || active {
		return
	}
	_ = s.Store.SetMachineStatus(machineID, model.MachineReady)
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
		name := ""
		if in.Inventory != nil {
			name = in.Inventory.IdentityName()
		}
		if name == "" && !genericMachineName(in.Hostname, in.MAC) {
			name = strings.TrimSpace(in.Hostname)
		}
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
		if m.Inventory != nil && in.Inventory != nil {
			bmc.FillIdentityGaps(in.Inventory, m.Inventory)
		}
		_ = s.Store.TouchMachine(m.ID, in.IP, st, fw, in.AgentVersion, in.Inventory)
		m, _ = s.Store.GetMachine(m.ID)
		if next := autoMachineName(m.Name, m.MAC, m.Inventory); next != m.Name {
			m.Name = next
			_ = s.Store.UpsertMachine(&m)
		}
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
	if j.ImageID != "" {
		if img, err := s.Store.GetImage(j.ImageID); err == nil && img.IsWindows() {
			writeJSON(w, 200, map[string]any{"job": nil})
			return
		}
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
	if r.Method == http.MethodGet {
		in.Progress, _ = strconv.Atoi(r.URL.Query().Get("progress"))
		in.Message = r.URL.Query().Get("message")
	} else if err := readJSON(r, &in); err != nil {
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
	if j.Status == model.JobPending {
		j.Status = model.JobRunning
		if j.StartedAt == nil {
			now := time.Now().UTC()
			j.StartedAt = &now
		}
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
	if r.Method == http.MethodGet {
		ok := strings.ToLower(r.URL.Query().Get("ok"))
		in.OK = ok == "1" || ok == "true" || ok == "yes"
		in.Message = r.URL.Query().Get("message")
	} else if err := readJSON(r, &in); err != nil {
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
	_, _ = w.Write([]byte(s.Netboot.MenuScriptBase(httpBase(r, s.Netboot.PublicURL()))))
}

func (s *Server) ipxeScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(s.Netboot.ScriptForBase(r.URL.Query().Get("mac"), r.URL.Query().Get("arch"), r.URL.Query().Get("platform"), httpBase(r, s.Netboot.PublicURL()))))
}

func httpBase(r *http.Request, fallback string) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return strings.TrimRight(fallback, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
		scheme = p
	}
	return scheme + "://" + host
}

func (s *Server) ramosStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write(s.Netboot.RamosStart(r.URL.Query().Get("mac")))
}

func (s *Server) cidataUserData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/cloud-config; charset=utf-8")
	_, _ = w.Write(s.Netboot.CIDataUserData(r.PathValue("mac")))
}

func (s *Server) cidataMetaData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(s.Netboot.CIDataMetaData(r.PathValue("mac")))
}

func (s *Server) cidataVendorData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/cloud-config; charset=utf-8")
	_, _ = w.Write(s.Netboot.CIDataVendorData())
}

func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
