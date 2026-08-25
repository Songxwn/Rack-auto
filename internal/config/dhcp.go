package config

import (
	"fmt"
	"net"
	"strings"
)

func (d *DHCP) Normalize() {
	if d.ListenAddr == "" {
		d.ListenAddr = "0.0.0.0:67"
	}
	if d.LeaseSec <= 0 {
		d.LeaseSec = 3600
	}
	d.Interface = strings.TrimSpace(d.Interface)
	d.Subnet = strings.TrimSpace(d.Subnet)
	d.RangeStart = strings.TrimSpace(d.RangeStart)
	d.RangeEnd = strings.TrimSpace(d.RangeEnd)
	d.Router = strings.TrimSpace(d.Router)
	d.DNS = strings.TrimSpace(d.DNS)
	d.NextServer = strings.TrimSpace(d.NextServer)
}

func (d DHCP) Validate() error {
	d.Normalize()
	if !d.Enabled {
		return nil
	}
	if d.Interface == "" {
		return fmt.Errorf("请指定 DHCP 接入网卡")
	}
	start := net.ParseIP(d.RangeStart)
	end := net.ParseIP(d.RangeEnd)
	if start == nil || start.To4() == nil || end == nil || end.To4() == nil {
		return fmt.Errorf("地址池起止必须是 IPv4")
	}
	if ip4Less(end.To4(), start.To4()) {
		return fmt.Errorf("地址池结束地址不能小于起始地址")
	}
	if d.Subnet != "" {
		n, err := ParseIPv4Net(d.Subnet)
		if err != nil {
			return fmt.Errorf("网段无效: %w", err)
		}
		if !n.Contains(start) || !n.Contains(end) {
			return fmt.Errorf("地址池不在网段 %s 内", d.Subnet)
		}
	}
	if d.Router != "" {
		rip := net.ParseIP(d.Router)
		if rip == nil || rip.To4() == nil {
			return fmt.Errorf("网关必须是 IPv4")
		}
		if d.Subnet != "" {
			n, err := ParseIPv4Net(d.Subnet)
			if err == nil && !n.Contains(rip) {
				return fmt.Errorf("网关 %s 不在网段 %s 内（会导致 iPXE Network unreachable）", d.Router, d.Subnet)
			}
		}
	}
	if d.NextServer != "" && net.ParseIP(d.NextServer).To4() == nil {
		return fmt.Errorf("next-server 必须是 IPv4")
	}
	for _, dns := range d.DNSList() {
		if net.ParseIP(dns).To4() == nil {
			return fmt.Errorf("DNS 必须是 IPv4: %s", dns)
		}
	}
	return nil
}

func (d DHCP) DNSList() []string {
	var out []string
	for _, p := range strings.Split(d.DNS, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (d DHCP) Mask() net.IPMask {
	if n, err := ParseIPv4Net(d.Subnet); err == nil {
		return n.Mask
	}
	return net.CIDRMask(24, 32)
}

// EffectiveRouter returns an on-link gateway. Option 3 must be in the PXE
// subnet; a leftover default like 10.0.0.1 on 192.168.177.0/24 makes iPXE
// report "Network unreachable" (err 28086011) and then loop.
func (d DHCP) EffectiveRouter(fallback net.IP) string {
	inNet := func(ip net.IP) bool {
		if ip == nil {
			return false
		}
		n, err := ParseIPv4Net(d.Subnet)
		if err != nil {
			return true
		}
		return n.Contains(ip)
	}
	if ip := net.ParseIP(d.Router); ip != nil && inNet(ip) {
		return ip.To4().String()
	}
	if ip := net.ParseIP(d.NextServer); ip != nil && inNet(ip) {
		return ip.To4().String()
	}
	if inNet(fallback) {
		return fallback.To4().String()
	}
	return ""
}

func ParseIPv4Net(cidr string) (*net.IPNet, error) {
	ip, n, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, err
	}
	if ip.To4() == nil {
		return nil, fmt.Errorf("不是 IPv4 网段")
	}
	return n, nil
}

func SuggestPool(network string) (start, end string) {
	n, err := ParseIPv4Net(network)
	if err != nil {
		return "", ""
	}
	base := n.IP.To4()
	if base == nil {
		return "", ""
	}
	ones, bits := n.Mask.Size()
	if bits != 32 {
		return "", ""
	}
	size := 1 << uint(32-ones)
	if size >= 256 {
		s := append(net.IP{}, base...)
		e := append(net.IP{}, base...)
		s[3] = 100
		e[3] = 200
		if !n.Contains(s) {
			s = addIPv4(base, 10)
		}
		if !n.Contains(e) {
			e = addIPv4(base, uint32(size-2))
		}
		if ip4Less(e, s) {
			e = s
		}
		return s.String(), e.String()
	}
	return addIPv4(base, 2).String(), addIPv4(base, uint32(size-2)).String()
}

func addIPv4(ip net.IP, n uint32) net.IP {
	v := ip.To4()
	x := uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
	x += n
	return net.IPv4(byte(x>>24), byte(x>>16), byte(x>>8), byte(x)).To4()
}

func ip4Less(a, b net.IP) bool {
	a, b = a.To4(), b.To4()
	for i := 0; i < 4; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
