package netboot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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
	base := s.PublicURL()
	return fmt.Sprintf(`#!ipxe
isset ${proxydhcp/next-server} && set next-server ${proxydhcp/next-server} ||
echo Rack-auto network boot
set base %s
iseq ${platform} efi && set bootloader efi || set bootloader bios
chain ${base}/ipxe/script?mac=${mac}&arch=${buildarch}&platform=${platform} || goto failed
:failed
echo iPXE chain failed, retrying in 8s
sleep 8
reboot
`, base)
}

func (s *Service) ScriptFor(mac, arch, platform string) string {
	base := s.PublicURL()
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
	token := ""
	if s.Store != nil && s.Store.S != nil {
		token = s.Store.S.Setting("api_token", s.Cfg.APIToken)
	}
	return fmt.Sprintf(`#!ipxe
echo Rack-auto RAMOS (%s / %s)
set base %s
kernel ${base}/ramos/%s/vmlinuz-lts initrd=initramfs-lts modules=loop,squashfs,sd-mod,usb-storage,ext4,nvme,ahci,xfs,btrfs alpine_repo=${base}/ramos/alpine/%s/ ip=dhcp apkovl=${base}/ipxe/apkovl.tgz?mac=${mac} modloop=${base}/ramos/%s/modloop-lts console=tty0 console=ttyS0,115200 rackauto_url=${base} rackauto_token=%s rackauto_mac=${mac} rackauto_fw=%s
initrd ${base}/ramos/%s/initramfs-lts
boot
`, firmware, archDir, base, archDir, archDir, archDir, token, firmware, archDir)
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

func (s *Service) APKOVL(mac string) ([]byte, error) {
	base := s.PublicURL()
	token := s.Cfg.APIToken
	if s.Store != nil && s.Store.S != nil {
		token = s.Store.S.Setting("api_token", token)
	}
	script := fmt.Sprintf(`#!/bin/sh
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
SERVER="%s"
TOKEN="%s"
MAC="%s"
for x in $(cat /proc/cmdline 2>/dev/null); do
  case "$x" in
    rackauto_url=*) SERVER="${x#*=}" ;;
    rackauto_token=*) TOKEN="${x#*=}" ;;
    rackauto_mac=*) MAC="${x#*=}" ;;
  esac
done
mkdir -p /usr/local/bin /var/log
echo "rackauto overlay starting" > /var/log/rackauto.log
# wait for network
i=0
while [ $i -lt 30 ]; do
  wget -q -O /dev/null "${SERVER}/api/v1/health" && break
  i=$((i+1))
  sleep 2
done
ARCH=$(uname -m)
wget -q -O /usr/local/bin/rackauto-agent "${SERVER}/boot/agent/${ARCH}/rackauto-agent" || \
  wget -q -O /usr/local/bin/rackauto-agent "${SERVER}/boot/agent/x86_64/rackauto-agent"
chmod +x /usr/local/bin/rackauto-agent
apk add --no-cache --quiet parted e2fsprogs e2fsprogs-extra dosfstools sgdisk sfdisk lsblk dmidecode util-linux curl qemu-img grub grub-efi efibootmgr nvme-cli hdparm coreutils lsblk mount umount rsync openssh-keygen 2>/dev/null || true
exec /usr/local/bin/rackauto-agent --url "$SERVER" --token "$TOKEN" --mac "$MAC" >> /var/log/rackauto.log 2>&1
`, base, token, NormalizeMAC(mac))

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []struct {
		name string
		body string
		mode int64
	}{
		{"etc/hostname", "ramos\n", 0644},
		{"etc/motd", "Rack-auto RAMOS — in-memory provisioning environment\n", 0644},
		{"etc/local.d/rackauto.start", script, 0755},
		{"etc/runlevels/default/local", "", 0755},
	}
	for _, f := range files {
		body := []byte(f.body)
		hdr := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(body)), Uid: 0, Gid: 0, ModTime: time.Now()}
		if f.name == "etc/runlevels/default/local" {
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = "/etc/init.d/local"
			hdr.Size = 0
			if err := tw.WriteHeader(hdr); err != nil {
				return nil, err
			}
			continue
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(body); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
