package netboot

import (
	"net"
	"strconv"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/config"
)

type HostNIC struct {
	Name string     `json:"name"`
	MAC  string     `json:"mac"`
	MTU  int        `json:"mtu"`
	Up   bool       `json:"up"`
	IPv4 []HostAddr `json:"ipv4"`
}

type HostAddr struct {
	Address   string `json:"address"`
	Prefix    int    `json:"prefix"`
	CIDR      string `json:"cidr"`
	Network   string `json:"network"`
	PoolStart string `json:"pool_start"`
	PoolEnd   string `json:"pool_end"`
}

func ListNICs() ([]HostNIC, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := []HostNIC{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		n := HostNIC{
			Name: iface.Name,
			MAC:  strings.ToLower(iface.HardwareAddr.String()),
			MTU:  iface.MTU,
			Up:   iface.Flags&net.FlagUp != 0,
			IPv4: []HostAddr{},
		}
		addrs, err := iface.Addrs()
		if err != nil {
			out = append(out, n)
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
				continue
			}
			ones, _ := ipn.Mask.Size()
			network := &net.IPNet{IP: ipn.IP.Mask(ipn.Mask), Mask: ipn.Mask}
			start, end := config.SuggestPool(network.String())
			n.IPv4 = append(n.IPv4, HostAddr{
				Address:   ipn.IP.To4().String(),
				Prefix:    ones,
				CIDR:      ipn.IP.To4().String() + "/" + strconv.Itoa(ones),
				Network:   network.String(),
				PoolStart: start,
				PoolEnd:   end,
			})
		}
		out = append(out, n)
	}
	return out, nil
}

func InterfaceIPv4(name string) net.IP {
	if name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
			continue
		}
		return ipn.IP.To4()
	}
	return nil
}
