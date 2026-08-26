package netboot

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pin/tftp/v3"

	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/store"
)

type Service struct {
	Cfg   config.Config
	Store *StoreView
	tftp  *tftp.Server

	dhcpMu    sync.Mutex
	dhcpCfg   config.DHCP
	dhcpSrv   interface{ Close() error }
	dhcpErr   string
	dhcpOn    bool
	dhcpSince time.Time
}

type StoreView struct {
	S *store.Store
}

func (v *StoreView) MachineByMAC(mac string) (model.Machine, error) {
	if v == nil || v.S == nil {
		return model.Machine{}, fmt.Errorf("no store")
	}
	return v.S.GetMachineByMAC(mac)
}

func (v *StoreView) WindowsInstall(mac string) (job model.Job, img model.Image, spec model.InstallSpec, ok bool) {
	if v == nil || v.S == nil {
		return
	}
	m, err := v.MachineByMAC(mac)
	if err != nil {
		return
	}
	jobs, err := v.S.ListJobs(m.ID, 40)
	if err != nil {
		return
	}
	var pending, running model.Job
	var pendingImg, runningImg model.Image
	var haveP, haveR bool
	for _, j := range jobs {
		if j.Type != model.JobInstall {
			continue
		}
		if j.Status != model.JobPending && j.Status != model.JobRunning {
			continue
		}
		if j.ImageID == "" {
			continue
		}
		im, err := v.S.GetImage(j.ImageID)
		if err != nil || !im.IsWindows() {
			continue
		}
		if j.Status == model.JobPending {
			if !haveP || j.CreatedAt.Before(pending.CreatedAt) {
				pending, pendingImg, haveP = j, im, true
			}
			continue
		}
		if !haveR || j.CreatedAt.After(running.CreatedAt) {
			running, runningImg, haveR = j, im, true
		}
	}
	if haveP {
		spec, _ = model.ParseInstallSpec(pending.Params)
		return pending, pendingImg, spec, true
	}
	if haveR {
		spec, _ = model.ParseInstallSpec(running.Params)
		return running, runningImg, spec, true
	}
	return
}

func New(cfg config.Config, st *store.Store) *Service {
	s := &Service{Cfg: cfg, Store: &StoreView{S: st}, dhcpCfg: cfg.DHCP}
	s.dhcpCfg.Normalize()
	s.dhcpCfg = s.loadDHCPLocked()
	return s
}

func (s *Service) PublicURL() string {
	if s.Store != nil && s.Store.S != nil {
		return strings.TrimRight(s.Store.S.Setting("public_url", s.Cfg.PublicURL), "/")
	}
	return s.Cfg.PublicURL
}

func (s *Service) StartTFTP() error {
	read := func(filename string, rf io.ReaderFrom) error {
		filename = strings.TrimPrefix(filepath.ToSlash(filename), "/")
		if filename == "boot.ipxe" || filename == "auto.ipxe" {
			body := strings.NewReader(s.MenuScriptLocal())
			_, err := rf.ReadFrom(body)
			return err
		}
		path := filepath.Join(s.Cfg.TFTPDir(), filename)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = rf.ReadFrom(f)
		return err
	}
	write := func(filename string, wt io.WriterTo) error {
		return fmt.Errorf("tftp write disabled")
	}
	srv := tftp.NewServer(read, write)
	srv.SetTimeout(5 * time.Second)
	s.tftp = srv
	go func() {
		_ = srv.ListenAndServe(s.Cfg.TFTPListen)
	}()
	return nil
}

func inferServerIP(publicURL string) net.IP {
	u := strings.TrimPrefix(strings.TrimPrefix(publicURL, "https://"), "http://")
	host, _, _ := strings.Cut(u, ":")
	host, _, _ = strings.Cut(host, "/")
	ip := net.ParseIP(host)
	if ip == nil {
		return net.ParseIP("0.0.0.0")
	}
	return ip.To4()
}

type leasePool struct {
	mu    sync.Mutex
	start net.IP
	end   net.IP
	byMAC map[string]net.IP
	used  map[string]string
}

func newLeasePool(start, end net.IP) *leasePool {
	return &leasePool{start: start.To4(), end: end.To4(), byMAC: map[string]net.IP{}, used: map[string]string{}}
}

func (p *leasePool) Assign(mac net.HardwareAddr) net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := mac.String()
	if ip, ok := p.byMAC[key]; ok {
		return ip
	}
	cur := append(net.IP{}, p.start...)
	for !after(cur, p.end) {
		k := cur.String()
		if _, taken := p.used[k]; !taken {
			ip := append(net.IP{}, cur...)
			p.used[k] = key
			p.byMAC[key] = ip
			return ip
		}
		incIP(cur)
	}
	return append(net.IP{}, p.start...)
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			return
		}
	}
}

func after(a, b net.IP) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (s *Service) MenuScript() string {
	return s.MenuScriptBase(s.PublicURL())
}

func (s *Service) MenuScriptBase(base string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = s.PublicURL()
	}
	return fmt.Sprintf(`#!ipxe
isset ${proxydhcp/next-server} && set next-server ${proxydhcp/next-server} ||
echo Rack-auto local boot
set base %s
chain ${base}/ipxe/script?mac=${mac}&arch=${buildarch}&platform=${platform} || goto failed
:failed
echo iPXE chain failed, retrying in 8s
sleep 8
reboot
`, base)
}

// MenuScriptLocal is served from TFTP as boot.ipxe. It only talks to this
// control plane (next-server), never boot.ipxe.org.
func (s *Service) MenuScriptLocal() string {
	port := HTTPPort(s.PublicURL(), s.Cfg.Listen)
	return fmt.Sprintf(`#!ipxe
echo Rack-auto iPXE (local / offline)
isset ${next-server} || set next-server ${net0/next-server}
isset ${next-server} || set next-server ${net0/dhcp-server}
set base http://${next-server}:%s
echo chaining ${base}/ipxe/script
chain ${base}/ipxe/script?mac=${mac}&arch=${buildarch}&platform=${platform} || goto failed
:failed
echo failed to reach ${base} - check control-plane HTTP :%s
sleep 8
reboot
`, port, port)
}

func (s *Service) ScriptFor(mac, arch, platform string) string {
	return s.ScriptForBase(mac, arch, platform, s.PublicURL())
}

func (s *Service) ScriptForBase(mac, arch, platform, base string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = s.PublicURL()
	}
	mac = NormalizeMAC(mac)
	bootLocal := false
	firmware := platformToFirmware(platform)
	if s.Store != nil {
		if m, err := s.Store.MachineByMAC(mac); err == nil {
			if m.BootMode == model.BootDisk && m.Status == model.MachineProvisioned {
				bootLocal = true
			}
			if m.Firmware != "" {
				firmware = m.Firmware
			}
		}
	}
	if bootLocal {
		return `#!ipxe
echo Rack-auto: boot from local disk
sanboot --no-describe --drive 0x80 || exit
`
	}
	if s.Store != nil {
		if job, img, spec, ok := s.Store.WindowsInstall(mac); ok {
			return s.windowsPEScript(base, mac, arch, job, img, spec)
		}
	}
	archDir := ArchDir(arch)
	rel := s.Cfg.Bootstrap.UbuntuRelease
	if rel == "" {
		rel = "26.04"
	}
	token := s.Cfg.APIToken
	if s.Store != nil && s.Store.S != nil {
		token = s.Store.S.Setting("api_token", token)
	}
	layer := s.layerFSPath(archDir)
	return fmt.Sprintf(`#!ipxe
echo Rack-auto RAMOS (Ubuntu %s / %s / %s)
set base %s
echo casper.iso is squashfs layers only - not the 2.7G live-server ISO
kernel ${base}/ramos/ubuntu/%s/vmlinuz initrd=initrd boot=casper ip=dhcp iso-url=${base}/ramos/ubuntu/%s/casper.iso ignore_uuid noprompt autoinstall cloud-config-url=${base}/ipxe/cidata/${mac}/user-data layerfs-path=%s root=/dev/ram0 console=tty0 console=ttyS0,115200 --- rackauto_url=${base} rackauto_token=%s rackauto_mac=${mac}
initrd ${base}/ramos/ubuntu/%s/initrd
boot
`, rel, firmware, archDir, base, archDir, archDir, layer, token, archDir)
}

func (s *Service) windowsPEScript(base, mac, arch string, job model.Job, img model.Image, spec model.InstallSpec) string {
	_ = spec
	if ArchDir(arch) != "x86_64" {
		return `#!ipxe
echo Rack-auto: Windows Server netboot requires x86_64
sleep 10
exit
`
	}
	boot := "win/" + img.ID + "/boot.wim"
	if img.Inspect != nil && img.Inspect.BootWIM != "" {
		boot = img.Inspect.BootWIM
		if !strings.HasPrefix(boot, "win/") {
			boot = "win/" + img.ID + "/boot.wim"
		}
	}
	return fmt.Sprintf(`#!ipxe
echo Rack-auto Windows PE job %s
set base %s
kernel ${base}/winpe/wimboot index=1
initrd ${base}/images/%s boot.wim
initrd --name winpeshl.ini ${base}/ipxe/windows/%s/winpeshl.ini winpeshl.ini
initrd --name startnet.cmd ${base}/ipxe/windows/%s/startnet.cmd startnet.cmd
initrd --name diskpart.txt ${base}/ipxe/windows/%s/diskpart.txt diskpart.txt
initrd --name install.cmd ${base}/ipxe/windows/%s/install.cmd install.cmd
initrd --name complete.json ${base}/ipxe/windows/%s/complete.json complete.json
initrd --name fail.json ${base}/ipxe/windows/%s/fail.json fail.json
boot
`, job.ID, base, boot, mac, mac, mac, mac, mac, mac)
}

func (s *Service) layerFSPath(archDir string) string {
	const fallback = "ubuntu-server-minimal.ubuntu-server.installer.generic.squashfs"
	if s.Cfg.DataDir == "" {
		return fallback
	}
	b, err := os.ReadFile(filepath.Join(s.Cfg.RAMOSDir(), "ubuntu", archDir, "layerfs-path"))
	if err != nil {
		return fallback
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return fallback
	}
	return p
}

func platformToFirmware(p string) string {
	if strings.Contains(strings.ToLower(p), "efi") {
		return model.FirmwareUEFI
	}
	return model.FirmwareBIOS
}

func ArchDir(arch string) string {
	switch strings.ToLower(arch) {
	case "arm64", "aarch64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

func NormalizeMAC(mac string) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	mac = strings.NewReplacer("-", ":", ".", ":").Replace(mac)
	return mac
}

func (s *Service) CIDataUserData(mac string) []byte {
	base := s.PublicURL()
	mac = NormalizeMAC(mac)
	url := fmt.Sprintf("%s/ipxe/ramos-start.sh?mac=%s", base, mac)
	curl := fmt.Sprintf("curl -fL --retry 20 --retry-delay 2 -o /tmp/ramos.sh %s", shQuote(url))
	body := fmt.Sprintf(`#cloud-config
autoinstall:
  version: 1
  interactive-sections: []
  refresh-installer:
    update: false
  early-commands:
    - %s
    - /bin/bash /tmp/ramos.sh
`, strconv.Quote(curl))
	return []byte(body)
}

func (s *Service) CIDataMetaData(mac string) []byte {
	mac = NormalizeMAC(mac)
	id := strings.NewReplacer(":", "", "-", "").Replace(mac)
	if id == "" {
		id = "ramos"
	}
	return []byte("instance-id: ramos-" + id + "\nlocal-hostname: ramos\n")
}

func (s *Service) CIDataVendorData() []byte {
	return []byte("#cloud-config\n")
}

func (s *Service) RamosStart(mac string) []byte {
	base := s.PublicURL()
	token := s.Cfg.APIToken
	if s.Store != nil && s.Store.S != nil {
		token = s.Store.S.Setting("api_token", token)
	}
	mac = NormalizeMAC(mac)
	return []byte(ramosStayScript(base, token, mac))
}

func ramosStayScript(server, token, mac string) string {
	return fmt.Sprintf(`#!/bin/bash
# Stay in the Ubuntu installer RAM environment and run the agent.
# Never return — otherwise Subiquity would continue and could wipe disks.
set +e
trap 'echo "RAMOS holding (agent stopped)"; sleep infinity' EXIT
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export DEBIAN_FRONTEND=noninteractive
SERVER=%s
TOKEN=%s
MAC=%s
mkdir -p /usr/local/bin /var/log
log() { echo "$*"; echo "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ 2>/dev/null || date) $*" >>/var/log/rackauto.log; }
log "RAMOS starting mac=$MAC server=$SERVER"

ARCH=$(uname -m)
case "$ARCH" in aarch64|arm64) A=aarch64 ;; *) A=x86_64 ;; esac
AGENT=/usr/local/bin/rackauto-agent
ok=
for u in "${SERVER}/boot/agent/${A}/rackauto-agent" "${SERVER}/boot/agent/x86_64/rackauto-agent"; do
  log "download $u"
  if curl -fL --connect-timeout 5 --max-time 60 --retry 8 --retry-delay 2 -o "$AGENT" "$u"; then
    ok=1
    break
  fi
done
if [ -z "$ok" ] || [ ! -s "$AGENT" ]; then
  log "ERROR: cannot download rackauto-agent from ${SERVER}/boot/agent/ (copy Release agent to data/agent/<arch>/rackauto-agent on the control plane)"
  sleep infinity
fi
chmod +x "$AGENT"

# Optional extras (sgdisk / vfat / grub). Do not block registration or qcow2 write.
( apt-get update -qq || true
  apt-get install -y -qq qemu-utils gdisk efibootmgr dosfstools e2fsprogs || true
) >/var/log/rackauto-apt.log 2>&1 &

log "starting rackauto-agent"
"$AGENT" --url "$SERVER" --token "$TOKEN" --mac "$MAC" 2>&1 | tee -a /var/log/rackauto.log
log "agent exited"
sleep infinity
`, shQuote(server), shQuote(token), shQuote(mac))
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

