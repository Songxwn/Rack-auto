package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/osprofile"
)

func ApplyNetwork(root string, cfg model.NetConfig, backend string) error {
	_ = os.MkdirAll(filepath.Join(root, "etc/cloud/cloud.cfg.d"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "etc/cloud/cloud.cfg.d/99-disable-network-config.cfg"), []byte("network: {config: disabled}\n"), 0644)
	if matches, err := filepath.Glob(filepath.Join(root, "etc/netplan/*cloud-init*")); err == nil {
		for _, p := range matches {
			_ = os.Remove(p)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(root, "etc/NetworkManager/system-connections/*cloud-init*")); err == nil {
		for _, p := range matches {
			_ = os.Remove(p)
		}
	}
	eths, _, _ := planNICs(cfg)
	if err := writePersistentNet(root, eths); err != nil {
		return err
	}
	switch backend {
	case osprofile.Ifupdown:
		_ = os.MkdirAll(filepath.Join(root, "etc/network"), 0o755)
		if err := os.WriteFile(filepath.Join(root, "etc/network/interfaces"), []byte(Ifupdown(cfg)), 0644); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(root, "etc/netplan/99-rackauto.yaml"))
	case osprofile.NM:
		_ = os.Remove(filepath.Join(root, "etc/netplan/99-rackauto.yaml"))
		return writeNM(root, cfg)
	case osprofile.Ifcfg:
		_ = os.Remove(filepath.Join(root, "etc/netplan/99-rackauto.yaml"))
		return writeIfcfg(root, cfg)
	default:
		_ = os.MkdirAll(filepath.Join(root, "etc/netplan"), 0o755)
		if err := os.WriteFile(filepath.Join(root, "etc/netplan/99-rackauto.yaml"), []byte(Netplan(cfg)), 0600); err != nil {
			return err
		}
	}
	return nil
}

func Netplan(cfg model.NetConfig) string {
	eths, bonds, vlans := planNICs(cfg)
	var b strings.Builder
	b.WriteString("network:\n  version: 2\n  renderer: networkd\n")
	if len(eths) == 0 && len(bonds) == 0 && len(vlans) == 0 {
		b.WriteString("  ethernets:\n    id0:\n      match:\n        name: \"e*\"\n      dhcp4: true\n")
		return b.String()
	}
	if len(eths) > 0 {
		b.WriteString("  ethernets:\n")
		for i, n := range eths {
			writeNetplanIface(&b, n, i, "    ")
		}
	}
	if len(bonds) > 0 {
		b.WriteString("  bonds:\n")
		for i, n := range bonds {
			writeNetplanIface(&b, n, i, "    ")
			mode := n.BondMode
			if mode == "" {
				mode = "802.3ad"
			}
			b.WriteString("      parameters:\n")
			fmt.Fprintf(&b, "        mode: %s\n", mode)
			b.WriteString("        mii-monitor-interval: 100\n")
			if mode == "802.3ad" {
				b.WriteString("        lacp-rate: fast\n")
			}
			if len(n.BondMembers) > 0 {
				fmt.Fprintf(&b, "      interfaces: [%s]\n", strings.Join(n.BondMembers, ", "))
			}
		}
	}
	if len(vlans) > 0 {
		b.WriteString("  vlans:\n")
		for i, n := range vlans {
			writeNetplanIface(&b, n, i, "    ")
			id := n.VLANID
			if id <= 0 {
				id = 1
			}
			fmt.Fprintf(&b, "      id: %d\n", id)
			parent := n.Parent
			if parent == "" {
				parent = "eth0"
			}
			fmt.Fprintf(&b, "      link: %s\n", parent)
		}
	}
	return b.String()
}

func writeNetplanIface(b *strings.Builder, n model.NICConfig, i int, pad string) {
	name := ifaceName(n, i)
	fmt.Fprintf(b, "%s%s:\n", pad, name)
	if n.Type() == model.NICEthernet && n.MAC != "" {
		fmt.Fprintf(b, "%s  match:\n%s    macaddress: \"%s\"\n%s  set-name: %s\n", pad, pad, strings.ToLower(n.MAC), pad, name)
	}
	if n.MTU > 0 {
		fmt.Fprintf(b, "%s  mtu: %d\n", pad, n.MTU)
	}
	switch {
	case isNone(n):
		b.WriteString(pad + "  dhcp4: false\n")
	case isStatic(n):
		b.WriteString(pad + "  dhcp4: false\n")
		fmt.Fprintf(b, "%s  addresses: [%s]\n", pad, n.Address)
		if n.Gateway != "" {
			fmt.Fprintf(b, "%s  routes:\n%s    - to: default\n%s      via: %s\n", pad, pad, pad, n.Gateway)
		}
		if len(n.DNS) > 0 {
			fmt.Fprintf(b, "%s  nameservers:\n%s    addresses: [%s]\n", pad, pad, strings.Join(n.DNS, ", "))
		}
	default:
		b.WriteString(pad + "  dhcp4: true\n")
	}
}

func Ifupdown(cfg model.NetConfig) string {
	eths, bonds, vlans := planNICs(cfg)
	var b strings.Builder
	b.WriteString("auto lo\niface lo inet loopback\n\n")
	if len(eths) == 0 && len(bonds) == 0 && len(vlans) == 0 {
		b.WriteString("auto eth0\niface eth0 inet dhcp\n")
		return b.String()
	}
	bondOf := map[string]string{}
	for _, n := range bonds {
		bn := ifaceName(n, 0)
		for _, m := range n.BondMembers {
			bondOf[m] = bn
		}
	}
	for i, n := range eths {
		writeIfupdownIface(&b, n, i, bondOf[ifaceName(n, i)])
	}
	for i, n := range bonds {
		writeIfupdownIface(&b, n, i, "")
	}
	for i, n := range vlans {
		writeIfupdownIface(&b, n, i, "")
	}
	return b.String()
}

func writeIfupdownIface(b *strings.Builder, n model.NICConfig, i int, bondMaster string) {
	name := ifaceName(n, i)
	fmt.Fprintf(b, "auto %s\n", name)
	switch {
	case isNone(n) || bondMaster != "":
		fmt.Fprintf(b, "iface %s inet manual\n", name)
	case isStatic(n):
		addr, mask := splitCIDR(n.Address)
		fmt.Fprintf(b, "iface %s inet static\n  address %s\n  netmask %s\n", name, addr, mask)
		if n.Gateway != "" {
			fmt.Fprintf(b, "  gateway %s\n", n.Gateway)
		}
		if len(n.DNS) > 0 {
			fmt.Fprintf(b, "  dns-nameservers %s\n", strings.Join(n.DNS, " "))
		}
	default:
		fmt.Fprintf(b, "iface %s inet dhcp\n", name)
	}
	if n.MTU > 0 {
		fmt.Fprintf(b, "  mtu %d\n", n.MTU)
	}
	if bondMaster != "" {
		fmt.Fprintf(b, "  bond-master %s\n", bondMaster)
	}
	if n.Type() == model.NICBond {
		mode := n.BondMode
		if mode == "" {
			mode = "802.3ad"
		}
		b.WriteString("  bond-slaves none\n")
		fmt.Fprintf(b, "  bond-mode %s\n  bond-miimon 100\n", mode)
		if mode == "802.3ad" {
			b.WriteString("  bond-lacp-rate 1\n")
		}
	}
	if n.Type() == model.NICVLAN {
		parent := n.Parent
		if parent == "" {
			parent = "eth0"
		}
		fmt.Fprintf(b, "  vlan-raw-device %s\n", parent)
	}
	b.WriteString("\n")
}

// planNICs remaps physical NICs that have a MAC onto stable names nic0, nic1, …
// so the installed OS does not inherit RAMOS (Ubuntu live) interface names.
func planNICs(cfg model.NetConfig) (eths, bonds, vlans []model.NICConfig) {
	eths, bonds, vlans = classifyNICs(cfg)
	alias := map[string]string{}
	macIdx := 0
	for i := range eths {
		orig := strings.TrimSpace(eths[i].Name)
		logical := orig
		if strings.TrimSpace(eths[i].MAC) != "" {
			logical = fmt.Sprintf("nic%d", macIdx)
			macIdx++
		}
		if logical == "" {
			logical = fmt.Sprintf("nic%d", macIdx)
			macIdx++
		}
		eths[i].Name = logical
		if orig != "" {
			alias[orig] = logical
		}
		alias[logical] = logical
	}
	for i := range bonds {
		orig := strings.TrimSpace(bonds[i].Name)
		if orig == "" {
			orig = "bond0"
		}
		bonds[i].Name = orig
		alias[orig] = orig
		for j, m := range bonds[i].BondMembers {
			m = strings.TrimSpace(m)
			if mapped, ok := alias[m]; ok {
				bonds[i].BondMembers[j] = mapped
			}
		}
	}
	for i := range vlans {
		origParent := strings.TrimSpace(vlans[i].Parent)
		parent := origParent
		if mapped, ok := alias[origParent]; ok {
			parent = mapped
		}
		if parent == "" {
			parent = "eth0"
		}
		vlans[i].Parent = parent
		origName := strings.TrimSpace(vlans[i].Name)
		autoOld := ""
		if origParent != "" && vlans[i].VLANID > 0 {
			autoOld = fmt.Sprintf("%s.%d", origParent, vlans[i].VLANID)
		}
		if origName == "" || origName == autoOld {
			vlans[i].Name = fmt.Sprintf("%s.%d", parent, vlans[i].VLANID)
		}
		if origName != "" {
			alias[origName] = vlans[i].Name
		}
		alias[vlans[i].Name] = vlans[i].Name
	}
	return
}

func writePersistentNet(root string, eths []model.NICConfig) error {
	var rules strings.Builder
	rules.WriteString("# rackauto: name NICs by MAC so the installed OS does not use RAMOS names\n")
	wrote := 0
	for _, nic := range eths {
		mac := strings.ToLower(strings.TrimSpace(nic.MAC))
		name := strings.TrimSpace(nic.Name)
		if mac == "" || name == "" {
			continue
		}
		if wrote == 0 {
			if err := os.MkdirAll(filepath.Join(root, "etc/systemd/network"), 0o755); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(root, "etc/udev/rules.d"), 0o755); err != nil {
				return err
			}
		}
		body := fmt.Sprintf("[Match]\nMACAddress=%s\n\n[Link]\nName=%s\nNamePolicy=\nAlternativeNamesPolicy=\n", mac, name)
		p := filepath.Join(root, "etc/systemd/network", "10-rackauto-"+name+".link")
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			return err
		}
		fmt.Fprintf(&rules, `SUBSYSTEM=="net", ACTION=="add", ATTR{address}=="%s", NAME="%s"`+"\n", mac, name)
		wrote++
	}
	if wrote == 0 {
		return nil
	}
	return os.WriteFile(filepath.Join(root, "etc/udev/rules.d/70-rackauto-net.rules"), []byte(rules.String()), 0644)
}

func classifyNICs(cfg model.NetConfig) (eths, bonds, vlans []model.NICConfig) {
	members := map[string]bool{}
	for _, n := range cfg.NICs {
		if n.Type() == model.NICBond {
			for _, m := range n.BondMembers {
				members[m] = true
			}
		}
	}
	seen := map[string]bool{}
	for _, n := range cfg.NICs {
		switch n.Type() {
		case model.NICBond:
			if n.Name == "" {
				n.Name = "bond0"
			}
			bonds = append(bonds, n)
		case model.NICVLAN:
			if n.Name == "" {
				parent := n.Parent
				if parent == "" {
					parent = "eth0"
				}
				n.Name = fmt.Sprintf("%s.%d", parent, n.VLANID)
			}
			vlans = append(vlans, n)
		default:
			if members[n.Name] {
				n.Method = "none"
			}
			eths = append(eths, n)
			if n.Name != "" {
				seen[n.Name] = true
			}
		}
	}
	for _, n := range bonds {
		for _, m := range n.BondMembers {
			if seen[m] || m == "" {
				continue
			}
			eths = append(eths, model.NICConfig{Kind: model.NICEthernet, Name: m, Method: "none"})
			seen[m] = true
		}
	}
	return
}

func ifaceName(n model.NICConfig, i int) string {
	if n.Name != "" {
		return n.Name
	}
	switch n.Type() {
	case model.NICBond:
		return "bond0"
	case model.NICVLAN:
		parent := n.Parent
		if parent == "" {
			parent = "eth0"
		}
		id := n.VLANID
		if id <= 0 {
			id = i + 1
		}
		return fmt.Sprintf("%s.%d", parent, id)
	default:
		return fmt.Sprintf("nic%d", i)
	}
}

func isStatic(n model.NICConfig) bool {
	return strings.EqualFold(n.Method, "static") && n.Address != ""
}

func isNone(n model.NICConfig) bool {
	m := strings.ToLower(n.Method)
	return m == "none" || m == "manual"
}

func splitCIDR(cidr string) (string, string) {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return cidr, "255.255.255.0"
	}
	masks := map[string]string{
		"8": "255.0.0.0", "16": "255.255.0.0", "24": "255.255.255.0",
		"25": "255.255.255.128", "26": "255.255.255.192", "27": "255.255.255.224",
		"28": "255.255.255.240", "29": "255.255.255.248", "30": "255.255.255.252", "32": "255.255.255.255",
	}
	if m, ok := masks[parts[1]]; ok {
		return parts[0], m
	}
	return parts[0], "255.255.255.0"
}

func prefixOf(addr string) string {
	_, rest, ok := strings.Cut(addr, "/")
	if !ok || rest == "" {
		return "24"
	}
	if _, err := strconv.Atoi(rest); err != nil {
		return "24"
	}
	return rest
}

func CloudInit(spec model.InstallSpec, hashed string) (userData, metaData string) {
	host := spec.Hostname
	if host == "" {
		host = spec.Network.Hostname
	}
	if host == "" {
		host = "rackauto"
	}
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	fmt.Fprintf(&b, "hostname: %s\n", host)
	b.WriteString("manage_etc_hosts: true\nssh_pwauth: true\ndisable_root: false\n")
	if spec.Timezone != "" {
		fmt.Fprintf(&b, "timezone: %s\n", spec.Timezone)
	}
	user := spec.Username
	if user == "" {
		user = "root"
	}
	fmt.Fprintf(&b, "users:\n  - name: %s\n    sudo: ALL=(ALL) NOPASSWD:ALL\n    groups: sudo,wheel,adm\n    shell: /bin/bash\n    lock_passwd: false\n", user)
	if hashed != "" {
		fmt.Fprintf(&b, "    passwd: \"%s\"\n", hashed)
	}
	if len(spec.SSHKeys) > 0 {
		b.WriteString("    ssh_authorized_keys:\n")
		for _, k := range spec.SSHKeys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			fmt.Fprintf(&b, "      - %s\n", k)
		}
	}
	b.WriteString("chpasswd:\n  expire: false\n")
	b.WriteString("package_update: false\n")
	b.WriteString("growpart:\n  mode: auto\n  devices: ['/']\nresize_rootfs: true\n")
	b.WriteString("runcmd:\n  - [modprobe, 8021q]\n  - [modprobe, bonding]\n")
	userData = b.String()
	metaData = fmt.Sprintf("instance-id: rackauto-%s\nlocal-hostname: %s\n", host, host)
	return
}
