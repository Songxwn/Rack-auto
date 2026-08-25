package netboot

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/Songxwn/Rack-auto/internal/config"
)

type DHCPStatus struct {
	Running   bool       `json:"running"`
	Enabled   bool       `json:"enabled"`
	Interface string     `json:"interface"`
	Listen    string     `json:"listen"`
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
}

func (s *Service) CurrentDHCP() config.DHCP {
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	return s.dhcpCfg
}

func (s *Service) DHCPStatus() DHCPStatus {
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	st := DHCPStatus{
		Running:   s.dhcpOn,
		Enabled:   s.dhcpCfg.Enabled,
		Interface: s.dhcpCfg.Interface,
		Listen:    s.dhcpCfg.ListenAddr,
		Error:     s.dhcpErr,
	}
	if s.dhcpOn && !s.dhcpSince.IsZero() {
		t := s.dhcpSince
		st.StartedAt = &t
	}
	return st
}

func (s *Service) loadDHCPLocked() config.DHCP {
	cfg := s.Cfg.DHCP
	cfg.Normalize()
	if s.Store != nil && s.Store.S != nil {
		raw := s.Store.S.Setting("dhcp", "")
		if raw != "" {
			var saved config.DHCP
			if json.Unmarshal([]byte(raw), &saved) == nil {
				saved.Normalize()
				cfg = saved
			}
		}
	}
	return cfg
}

func (s *Service) persistDHCP(d config.DHCP) error {
	if s.Store == nil || s.Store.S == nil {
		return nil
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.Store.S.SetSetting("dhcp", string(b))
}

func (s *Service) ApplyDHCP(d config.DHCP) error {
	d.Normalize()
	if err := d.Validate(); err != nil {
		return err
	}
	if err := s.persistDHCP(d); err != nil {
		return err
	}
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	s.dhcpCfg = d
	s.stopDHCPLocked()
	if !d.Enabled {
		s.dhcpErr = ""
		return nil
	}
	return s.startDHCPLocked()
}

func (s *Service) StopDHCP() {
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	s.stopDHCPLocked()
	s.dhcpCfg.Enabled = false
	s.dhcpErr = ""
	_ = s.persistDHCP(s.dhcpCfg)
}

func (s *Service) CloseDHCP() {
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	s.stopDHCPLocked()
}

func (s *Service) StartDHCP() error {
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	s.dhcpCfg = s.loadDHCPLocked()
	s.stopDHCPLocked()
	if !s.dhcpCfg.Enabled {
		return nil
	}
	return s.startDHCPLocked()
}

func (s *Service) stopDHCPLocked() {
	if s.dhcpSrv != nil {
		_ = s.dhcpSrv.Close()
		s.dhcpSrv = nil
	}
	s.dhcpOn = false
}

func (s *Service) startDHCPLocked() error {
	d := s.dhcpCfg
	start := net.ParseIP(d.RangeStart).To4()
	end := net.ParseIP(d.RangeEnd).To4()
	if start == nil || end == nil {
		err := fmt.Errorf("DHCP 地址池无效")
		s.dhcpErr = err.Error()
		return err
	}
	serverIP := s.nextServerIP(d)
	if serverIP == nil || serverIP.IsUnspecified() {
		err := fmt.Errorf("无法确定 next-server，请填写接入网卡上的 IPv4 或 next-server")
		s.dhcpErr = err.Error()
		return err
	}
	laddr := d.ListenAddr
	if laddr == "" {
		laddr = "0.0.0.0:67"
	}
	addr, err := net.ResolveUDPAddr("udp4", laddr)
	if err != nil {
		s.dhcpErr = err.Error()
		return err
	}
	leases := newLeasePool(start, end)
	mask := d.Mask()
	handler := s.dhcpHandler(d, serverIP, mask, leases)
	srv, err := server4.NewServer(d.Interface, addr, handler)
	if err != nil {
		s.dhcpErr = err.Error()
		return fmt.Errorf("绑定 DHCP 到网卡 %s 失败: %w", d.Interface, err)
	}
	s.dhcpSrv = srv
	s.dhcpOn = true
	s.dhcpErr = ""
	s.dhcpSince = time.Now().UTC()
	go s.serveDHCP(srv, d.Interface)
	log.Printf("DHCP 已在网卡 %s 上监听 %s  池 %s-%s  next-server %s", d.Interface, laddr, d.RangeStart, d.RangeEnd, serverIP)
	return nil
}

func (s *Service) serveDHCP(srv *server4.Server, iface string) {
	err := srv.Serve()
	s.dhcpMu.Lock()
	defer s.dhcpMu.Unlock()
	if s.dhcpSrv == srv {
		s.dhcpSrv = nil
		s.dhcpOn = false
		if err != nil && !isConnClosed(err) {
			s.dhcpErr = err.Error()
			log.Printf("DHCP (%s) 退出: %v", iface, err)
		}
	}
}

func (s *Service) nextServerIP(d config.DHCP) net.IP {
	if ip := net.ParseIP(d.NextServer); ip != nil && ip.To4() != nil {
		return ip.To4()
	}
	if ip := InterfaceIPv4(d.Interface); ip != nil {
		return ip
	}
	return inferServerIP(s.PublicURL())
}

func (s *Service) dhcpHandler(d config.DHCP, serverIP net.IP, mask net.IPMask, leases *leasePool) server4.Handler {
	router := net.ParseIP(d.Router).To4()
	dns := []net.IP{}
	for _, x := range d.DNSList() {
		if ip := net.ParseIP(x).To4(); ip != nil {
			dns = append(dns, ip)
		}
	}
	lease := uint32(d.LeaseSec)
	if lease == 0 {
		lease = 3600
	}
	return func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
		if m.OpCode != dhcpv4.OpcodeBootRequest {
			return
		}
		mt := m.MessageType()
		if mt != dhcpv4.MessageTypeDiscover && mt != dhcpv4.MessageTypeRequest {
			return
		}
		ip := leases.Assign(m.ClientHWAddr)
		filename := BootFilename(m)
		modifiers := []dhcpv4.Modifier{
			dhcpv4.WithYourIP(ip),
			dhcpv4.WithLeaseTime(lease),
			dhcpv4.WithServerIP(serverIP),
			dhcpv4.WithOption(dhcpv4.OptTFTPServerName(serverIP.String())),
			dhcpv4.WithOption(dhcpv4.OptBootFileName(filename)),
			dhcpv4.WithOption(dhcpv4.OptServerIdentifier(serverIP)),
			dhcpv4.WithNetmask(mask),
			func(p *dhcpv4.DHCPv4) { p.BootFileName = filename },
		}
		if router != nil {
			modifiers = append(modifiers, dhcpv4.WithRouter(router))
		}
		if len(dns) > 0 {
			modifiers = append(modifiers, dhcpv4.WithDNS(dns...))
		}
		reply, err := dhcpv4.NewReplyFromRequest(m, modifiers...)
		if err != nil {
			return
		}
		if mt == dhcpv4.MessageTypeDiscover {
			reply.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer))
		} else {
			reply.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeAck))
		}
		_, _ = conn.WriteTo(reply.ToBytes(), peer)
	}
}

func BootFilename(m *dhcpv4.DHCPv4) string {
	for _, arch := range m.ClientArch() {
		switch arch {
		case iana.EFI_X86_64, iana.EFI_BC, iana.EFI_X86_64_HTTP, iana.EFI_ARM64, iana.EFI_ARM64_HTTP:
			return "ipxe.efi"
		case iana.EFI_IA32:
			return "ipxe-ia32.efi"
		}
	}
	return "undionly.kpxe"
}

func isConnClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "closed") || strings.Contains(msg, "use of closed")
}
