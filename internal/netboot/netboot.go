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
echo failed to reach ${base} — check control-plane HTTP :%s
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
kernel ${base}/ramos/ubuntu/%s/vmlinuz initrd=initrd boot=casper ip=dhcp iso-url=${base}/ramos/ubuntu/%s/casper.iso ignore_uuid noprompt cloud-init=disabled layerfs-path=%s root=/dev/ram0 console=tty0 console=ttyS0,115200 --- rackauto_url=${base} rackauto_token=%s rackauto_mac=${mac}
initrd ${base}/ramos/ubuntu/%s/initrd
boot
`, rel, firmware, archDir, base, archDir, archDir, layer, token, archDir)
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
	return []byte(fmt.Sprintf(`#!/bin/bash
# Rack-auto RAMOS: stay in the Ubuntu installer RAM environment and run the agent.
# Never return — otherwise subiquity would continue and could wipe disks.
trap 'echo "RAMOS holding (agent stopped)"; sleep infinity' EXIT
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
SERVER=%s
TOKEN=%s
MAC=%s
mkdir -p /usr/local/bin /var/log
exec >>/var/log/rackauto.log 2>&1
echo "rackauto RAMOS starting $(date -Is)"
i=0
while [ "$i" -lt 60 ]; do
  curl -fsS "${SERVER}/api/v1/health" >/dev/null && break
  i=$((i+1))
  sleep 2
done
ARCH=$(uname -m)
case "$ARCH" in
  aarch64|arm64) A=aarch64 ;;
  *) A=x86_64 ;;
esac
curl -fL -o /usr/local/bin/rackauto-agent "${SERVER}/boot/agent/${A}/rackauto-agent" || \
  curl -fL -o /usr/local/bin/rackauto-agent "${SERVER}/boot/agent/x86_64/rackauto-agent"
chmod +x /usr/local/bin/rackauto-agent
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null 2>&1 || true
apt-get install -y -qq qemu-utils efibootmgr dosfstools e2fsprogs >/dev/null 2>&1 || true
echo "starting rackauto-agent"
/usr/local/bin/rackauto-agent --url "$SERVER" --token "$TOKEN" --mac "$MAC" || true
echo "agent exited"
sleep infinity
`, shQuote(base), shQuote(token), shQuote(mac)))
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

