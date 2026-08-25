package provision

import (
	"fmt"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func Netplan(cfg model.NetConfig) string {
	var b strings.Builder
	b.WriteString("network:\n  version: 2\n  ethernets:\n")
	if len(cfg.NICs) == 0 {
		b.WriteString("    id0:\n      match:\n        name: \"e*\"\n      dhcp4: true\n")
		return b.String()
	}
	for i, n := range cfg.NICs {
		name := n.Name
		if name == "" {
			name = fmt.Sprintf("nic%d", i)
		}
		fmt.Fprintf(&b, "    %s:\n", name)
		if n.MAC != "" {
			fmt.Fprintf(&b, "      match:\n        macaddress: \"%s\"\n      set-name: %s\n", strings.ToLower(n.MAC), name)
		}
		if n.MTU > 0 {
			fmt.Fprintf(&b, "      mtu: %d\n", n.MTU)
		}
		if strings.EqualFold(n.Method, "static") && n.Address != "" {
			b.WriteString("      dhcp4: false\n")
			fmt.Fprintf(&b, "      addresses: [%s]\n", n.Address)
			if n.Gateway != "" {
				fmt.Fprintf(&b, "      routes:\n        - to: default\n          via: %s\n", n.Gateway)
			}
			if len(n.DNS) > 0 {
				fmt.Fprintf(&b, "      nameservers:\n        addresses: [%s]\n", strings.Join(n.DNS, ", "))
			}
		} else {
			b.WriteString("      dhcp4: true\n")
		}
	}
	return b.String()
}

func Ifupdown(cfg model.NetConfig) string {
	var b strings.Builder
	b.WriteString("auto lo\niface lo inet loopback\n\n")
	if len(cfg.NICs) == 0 {
		b.WriteString("auto eth0\niface eth0 inet dhcp\n")
		return b.String()
	}
	for i, n := range cfg.NICs {
		name := n.Name
		if name == "" {
			name = fmt.Sprintf("eth%d", i)
		}
		fmt.Fprintf(&b, "auto %s\n", name)
		if strings.EqualFold(n.Method, "static") && n.Address != "" {
			addr, mask := splitCIDR(n.Address)
			fmt.Fprintf(&b, "iface %s inet static\n  address %s\n  netmask %s\n", name, addr, mask)
			if n.Gateway != "" {
				fmt.Fprintf(&b, "  gateway %s\n", n.Gateway)
			}
			if len(n.DNS) > 0 {
				fmt.Fprintf(&b, "  dns-nameservers %s\n", strings.Join(n.DNS, " "))
			}
		} else {
			fmt.Fprintf(&b, "iface %s inet dhcp\n", name)
		}
		if n.MTU > 0 {
			fmt.Fprintf(&b, "  mtu %d\n", n.MTU)
		}
		b.WriteString("\n")
	}
	return b.String()
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
	b.WriteString("manage_etc_hosts: true\nssh_pwauth: true\n")
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
	userData = b.String()
	metaData = fmt.Sprintf("instance-id: rackauto-%s\nlocal-hostname: %s\n", host, host)
	return
}
