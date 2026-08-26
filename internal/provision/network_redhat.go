package provision

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func writeNM(root string, cfg model.NetConfig) error {
	dir := filepath.Join(root, "etc/NetworkManager/system-connections")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Join(root, "etc/NetworkManager/conf.d"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "etc/NetworkManager/conf.d/99-rackauto.conf"), []byte("[main]\nno-auto-default=*\n"), 0644)
	eths, bonds, vlans := planNICs(cfg)
	bondOf := map[string]string{}
	for _, n := range bonds {
		bn := ifaceName(n, 0)
		for _, m := range n.BondMembers {
			bondOf[m] = bn
		}
	}
	write := func(name, body string) error {
		p := filepath.Join(dir, name+".nmconnection")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return err
		}
		return os.Chmod(p, 0o600)
	}
	for i, n := range eths {
		name := ifaceName(n, i)
		if err := write(name, nmConnection(n, i, bondOf[name])); err != nil {
			return err
		}
	}
	for i, n := range bonds {
		if err := write(ifaceName(n, i), nmConnection(n, i, "")); err != nil {
			return err
		}
	}
	for i, n := range vlans {
		if err := write(ifaceName(n, i), nmConnection(n, i, "")); err != nil {
			return err
		}
	}
	if len(eths)+len(bonds)+len(vlans) == 0 {
		return write("eth0", nmDHCP("eth0"))
	}
	return nil
}

func nmDHCP(name string) string {
	return fmt.Sprintf("[connection]\nid=%s\nuuid=%s\ntype=ethernet\ninterface-name=%s\nautoconnect=true\n\n[ipv4]\nmethod=auto\n\n[ipv6]\nmethod=ignore\n", name, connUUID(name), name)
}

func nmConnection(n model.NICConfig, i int, master string) string {
	name := ifaceName(n, i)
	var b strings.Builder
	kind := n.Type()
	nmType := "ethernet"
	switch kind {
	case model.NICBond:
		nmType = "bond"
	case model.NICVLAN:
		nmType = "vlan"
	}
	fmt.Fprintf(&b, "[connection]\nid=%s\nuuid=%s\ntype=%s\ninterface-name=%s\nautoconnect=true\n", name, connUUID(name), nmType, name)
	if master != "" {
		fmt.Fprintf(&b, "master=%s\nslave-type=bond\n", master)
	}
	b.WriteString("\n")
	switch kind {
	case model.NICBond:
		mode := n.BondMode
		if mode == "" {
			mode = "802.3ad"
		}
		fmt.Fprintf(&b, "[bond]\nmode=%s\nmiimon=100\n", mode)
		if mode == "802.3ad" {
			b.WriteString("lacp_rate=fast\n")
		}
		b.WriteString("\n")
	case model.NICVLAN:
		parent := n.Parent
		if parent == "" {
			parent = "eth0"
		}
		id := n.VLANID
		if id <= 0 {
			id = 1
		}
		fmt.Fprintf(&b, "[vlan]\nid=%d\nparent=%s\n\n", id, parent)
	default:
		if n.MAC != "" {
			fmt.Fprintf(&b, "[ethernet]\nmac-address=%s\n\n", strings.ToUpper(n.MAC))
		}
	}
	if n.MTU > 0 {
		fmt.Fprintf(&b, "[ethernet]\nmtu=%d\n\n", n.MTU)
	}
	writeNMIPv4(&b, n, master != "")
	b.WriteString("[ipv6]\nmethod=ignore\n")
	return b.String()
}

func writeNMIPv4(b *strings.Builder, n model.NICConfig, slave bool) {
	b.WriteString("[ipv4]\n")
	if slave || isNone(n) {
		b.WriteString("method=disabled\n\n")
		return
	}
	if isStatic(n) {
		b.WriteString("method=manual\n")
		fmt.Fprintf(b, "address1=%s", n.Address)
		if n.Gateway != "" {
			fmt.Fprintf(b, ",%s", n.Gateway)
		}
		b.WriteString("\n")
		if len(n.DNS) > 0 {
			fmt.Fprintf(b, "dns=%s;\n", strings.Join(n.DNS, ";"))
		}
		b.WriteString("\n")
		return
	}
	b.WriteString("method=auto\n\n")
}

func writeIfcfg(root string, cfg model.NetConfig) error {
	dir := filepath.Join(root, "etc/sysconfig/network-scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(root, "etc/sysconfig/network"), []byte("NETWORKING=yes\nNOZEROCONF=yes\n"), 0644)
	eths, bonds, vlans := planNICs(cfg)
	bondOf := map[string]string{}
	for _, n := range bonds {
		bn := ifaceName(n, 0)
		for _, m := range n.BondMembers {
			bondOf[m] = bn
		}
	}
	write := func(name, body string) error {
		return os.WriteFile(filepath.Join(dir, "ifcfg-"+name), []byte(body), 0644)
	}
	for i, n := range eths {
		name := ifaceName(n, i)
		if err := write(name, ifcfgFile(n, i, bondOf[name])); err != nil {
			return err
		}
	}
	for i, n := range bonds {
		if err := write(ifaceName(n, i), ifcfgFile(n, i, "")); err != nil {
			return err
		}
	}
	for i, n := range vlans {
		if err := write(ifaceName(n, i), ifcfgFile(n, i, "")); err != nil {
			return err
		}
	}
	if len(eths)+len(bonds)+len(vlans) == 0 {
		return write("eth0", "DEVICE=eth0\nBOOTPROTO=dhcp\nONBOOT=yes\nNM_CONTROLLED=yes\n")
	}
	return nil
}

func ifcfgFile(n model.NICConfig, i int, master string) string {
	name := ifaceName(n, i)
	var b strings.Builder
	fmt.Fprintf(&b, "DEVICE=%s\nONBOOT=yes\nNM_CONTROLLED=yes\n", name)
	switch n.Type() {
	case model.NICBond:
		mode := n.BondMode
		if mode == "" {
			mode = "802.3ad"
		}
		b.WriteString("TYPE=Bond\nBONDING_MASTER=yes\n")
		fmt.Fprintf(&b, "BONDING_OPTS=\"mode=%s miimon=100", mode)
		if mode == "802.3ad" {
			b.WriteString(" lacp_rate=fast")
		}
		b.WriteString("\"\n")
	case model.NICVLAN:
		parent := n.Parent
		if parent == "" {
			parent = "eth0"
		}
		id := n.VLANID
		if id <= 0 {
			id = 1
		}
		fmt.Fprintf(&b, "VLAN=yes\nTYPE=Vlan\nPHYSDEV=%s\nVLAN_ID=%d\n", parent, id)
	default:
		b.WriteString("TYPE=Ethernet\n")
		if n.MAC != "" {
			fmt.Fprintf(&b, "HWADDR=%s\n", strings.ToUpper(n.MAC))
		}
	}
	if master != "" {
		fmt.Fprintf(&b, "MASTER=%s\nSLAVE=yes\nBOOTPROTO=none\n", master)
		return b.String()
	}
	if n.MTU > 0 {
		fmt.Fprintf(&b, "MTU=%d\n", n.MTU)
	}
	switch {
	case isNone(n):
		b.WriteString("BOOTPROTO=none\n")
	case isStatic(n):
		addr, mask := splitCIDR(n.Address)
		b.WriteString("BOOTPROTO=none\n")
		fmt.Fprintf(&b, "IPADDR=%s\nNETMASK=%s\nPREFIX=%s\n", addr, mask, prefixOf(n.Address))
		if n.Gateway != "" {
			fmt.Fprintf(&b, "GATEWAY=%s\n", n.Gateway)
		}
		for i, d := range n.DNS {
			fmt.Fprintf(&b, "DNS%d=%s\n", i+1, d)
		}
	default:
		b.WriteString("BOOTPROTO=dhcp\n")
	}
	return b.String()
}

func connUUID(name string) string {
	h := sha1.Sum([]byte("rackauto-nm-" + name))
	h[6] = (h[6] & 0x0f) | 0x50
	h[8] = (h[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
