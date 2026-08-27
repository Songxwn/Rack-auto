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
	d.Router = d.EffectiveRouter(s.nextServerIP(d))
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
		err := fmt.Errorf("invalid DHCP pool")
		s.dhcpErr = err.Error()
		return err
	}
	serverIP := s.nextServerIP(d)
	if serverIP == nil || serverIP.IsUnspecified() {
		err := fmt.Errorf("cannot determine next-server; set IPv4 on the uplink NIC or next-server")
		s.dhcpErr = err.Error()
		return err
	}
	if gw := d.EffectiveRouter(serverIP); gw != d.Router {
		log.Printf("DHCP router %q is not in PXE subnet %s; using %s", d.Router, d.Subnet, gw)
		d.Router = gw
		s.dhcpCfg.Router = gw
		_ = s.persistDHCP(d)
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
		return fmt.Errorf("bind DHCP on %s: %w", d.Interface, err)
	}
	s.dhcpSrv = srv
	s.dhcpOn = true
	s.dhcpErr = ""
	s.dhcpSince = time.Now().UTC()
	go s.serveDHCP(srv, d.Interface)
	log.Printf("DHCP on %s listen %s  pool %s-%s  next-server %s  pxe_only=%v", d.Interface, laddr, d.RangeStart, d.RangeEnd, serverIP, d.PXEOnlyEnabled())
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
			log.Printf("DHCP (%s) exited: %v", iface, err)
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
		if d.PXEOnlyEnabled() && !IsPXEClient(m) {
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
		log.Printf("DHCP %s %s -> %s boot=%s", mt, m.ClientHWAddr, ip, filename)
		_, _ = conn.WriteTo(reply.ToBytes(), peer)
	}
}

func BootFilename(m *dhcpv4.DHCPv4) string {
	if IsIPXEClient(m) {
		return "boot.ipxe"
	}
	for _, arch := range m.ClientArch() {
		switch arch {
		case iana.EFI_ARM64, iana.EFI_ARM64_HTTP:
			return "ipxe-arm64.efi"
		case iana.EFI_X86_64, iana.EFI_BC, iana.EFI_X86_64_HTTP:
			return "ipxe.efi"
		case iana.EFI_IA32:
			return "ipxe-ia32.efi"
		}
	}
	return "undionly.kpxe"
}

func IsIPXEClient(m *dhcpv4.DHCPv4) bool {
	if m == nil {
		return false
	}
	for _, uc := range m.UserClass() {
		if strings.Contains(strings.ToLower(uc), "ipxe") {
			return true
		}
	}
	if strings.Contains(strings.ToLower(m.ClassIdentifier()), "ipxe") {
		return true
	}
	return m.GetOneOption(dhcpv4.OptionEtherboot) != nil
}

// IsPXEClient reports whether the packet looks like network boot (PXE / iPXE / HTTP boot).
// Intentionally broad so firmware that omits Option 60 still gets an Offer with bootfile.
func IsPXEClient(m *dhcpv4.DHCPv4) bool {
	if m == nil {
		return false
	}
	if IsIPXEClient(m) {
		return true
	}
	ci := strings.ToLower(m.ClassIdentifier())
	if strings.Contains(ci, "pxeclient") || strings.Contains(ci, "httpclient") {
		return true
	}
	if len(m.ClientArch()) > 0 {
		return true
	}
	if m.GetOneOption(dhcpv4.OptionClientNetworkInterfaceIdentifier) != nil {
		return true
	}
	if m.GetOneOption(dhcpv4.OptionClientMachineIdentifier) != nil {
		return true
	}
	if strings.TrimSpace(m.BootFileName) != "" {
		return true
	}
	// BIOS/UEFI PXE almost always asks for TFTP server / bootfile in the PRL.
	for _, code := range m.ParameterRequestList() {
		switch code {
		case dhcpv4.OptionTFTPServerName, dhcpv4.OptionBootfileName:
			return true
		}
	}
	return false
}

func IPXEChainURL(nextServer net.IP, publicURL, listen string) string {
	port := HTTPPort(publicURL, listen)
	host := ""
	if nextServer != nil && nextServer.To4() != nil && !nextServer.IsUnspecified() {
		host = nextServer.To4().String()
	} else {
		host = hostOf(publicURL)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	if port == "80" {
		return fmt.Sprintf("http://%s/ipxe/boot.ipxe", host)
	}
	return fmt.Sprintf("http://%s:%s/ipxe/boot.ipxe", host, port)
}

func HTTPPort(publicURL, listen string) string {
	if u := hostPortOf(publicURL); u != "" {
		if _, p, err := net.SplitHostPort(u); err == nil && p != "" {
			return p
		}
	}
	l := strings.TrimSpace(listen)
	if l != "" {
		if strings.HasPrefix(l, ":") {
			p := strings.TrimPrefix(l, ":")
			if p != "" {
				return p
			}
		}
		if _, p, err := net.SplitHostPort(l); err == nil && p != "" {
			return p
		}
	}
	return "8080"
}

func hostOf(publicURL string) string {
	hp := hostPortOf(publicURL)
	if hp == "" {
		return ""
	}
	h, _, err := net.SplitHostPort(hp)
	if err != nil {
		return hp
	}
	return h
}

func hostPortOf(publicURL string) string {
	u := strings.TrimPrefix(strings.TrimPrefix(publicURL, "https://"), "http://")
	u, _, _ = strings.Cut(u, "/")
	return strings.TrimSpace(u)
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
