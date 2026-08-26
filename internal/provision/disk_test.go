package provision_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func TestDefaultPartitions(t *testing.T) {
	uefi := provision.DefaultPartitions(model.FirmwareUEFI, "ubuntu", "24.04")
	if err := provision.Validate(uefi); err != nil {
		t.Fatal(err)
	}
	bios := provision.DefaultPartitions(model.FirmwareBIOS, "rocky", "9")
	if err := provision.Validate(bios); err != nil {
		t.Fatal(err)
	}
	if bios[1].FS != "xfs" {
		t.Fatalf("rocky root fs %s", bios[1].FS)
	}
}

func TestSGDiskAndFstab(t *testing.T) {
	parts := provision.DefaultPartitions(model.FirmwareUEFI, "ubuntu", "24.04")
	cmds := provision.SGDiskScript("/dev/sda", parts)
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "sgdisk") || !strings.Contains(joined, "ef00") {
		t.Fatalf("unexpected sgdisk script: %s", joined)
	}
	fstab := provision.Fstab(parts, "/dev/sda")
	if !strings.Contains(fstab, "/dev/sda2 / ext4") {
		t.Fatalf("fstab: %s", fstab)
	}
	if provision.PartitionPath("/dev/nvme0n1", 1) != "/dev/nvme0n1p1" {
		t.Fatal("nvme path")
	}
}

func TestNetplanStatic(t *testing.T) {
	out := provision.Netplan(model.NetConfig{NICs: []model.NICConfig{{
		MAC: "aa:bb:cc:dd:ee:ff", Name: "eth0", Method: "static",
		Address: "10.0.0.20/24", Gateway: "10.0.0.1", DNS: []string{"8.8.8.8"},
	}}})
	for _, s := range []string{"macaddress", "10.0.0.20/24", "10.0.0.1", "8.8.8.8"} {
		if !strings.Contains(out, s) {
			t.Fatalf("missing %s in %s", s, out)
		}
	}
}

func TestNetplanBondVLAN(t *testing.T) {
	out := provision.Netplan(model.NetConfig{NICs: []model.NICConfig{
		{Kind: model.NICBond, Name: "bond0", BondMode: "802.3ad", BondMembers: []string{"eth0", "eth1"}, Method: "none"},
		{Kind: model.NICVLAN, Parent: "bond0", VLANID: 100, Method: "static", Address: "10.10.10.5/24", Gateway: "10.10.10.1", DNS: []string{"8.8.8.8"}},
	}})
	for _, s := range []string{"bonds:", "bond0:", "interfaces: [eth0, eth1]", "vlans:", "id: 100", "link: bond0", "10.10.10.5/24", "ethernets:", "eth0:", "eth1:"} {
		if !strings.Contains(out, s) {
			t.Fatalf("missing %q in %s", s, out)
		}
	}
}

func TestIfupdownBondVLAN(t *testing.T) {
	out := provision.Ifupdown(model.NetConfig{NICs: []model.NICConfig{
		{Kind: model.NICBond, Name: "bond0", BondMembers: []string{"eth0", "eth1"}, Method: "none"},
		{Kind: model.NICVLAN, Parent: "bond0", VLANID: 200, Method: "static", Address: "10.0.0.8/24", Gateway: "10.0.0.1"},
	}})
	for _, s := range []string{"bond-master bond0", "bond-mode 802.3ad", "vlan-raw-device bond0", "iface bond0.200 inet static", "address 10.0.0.8"} {
		if !strings.Contains(out, s) {
			t.Fatalf("missing %q in %s", s, out)
		}
	}
}

func TestApplyNetworkBackends(t *testing.T) {
	cfg := model.NetConfig{NICs: []model.NICConfig{
		{Kind: model.NICBond, Name: "bond0", BondMembers: []string{"eth0", "eth1"}, Method: "dhcp"},
		{Kind: model.NICVLAN, Parent: "bond0", VLANID: 80, Method: "static", Address: "192.168.80.10/24"},
	}}
	root := t.TempDir()
	if err := provision.ApplyNetwork(root, cfg, "nm"); err != nil {
		t.Fatal(err)
	}
	bond := filepath.Join(root, "etc/NetworkManager/system-connections/bond0.nmconnection")
	vlan := filepath.Join(root, "etc/NetworkManager/system-connections/bond0.80.nmconnection")
	b, err := os.ReadFile(bond)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "type=bond") || !strings.Contains(string(b), "mode=802.3ad") {
		t.Fatalf("nm bond: %s", b)
	}
	v, err := os.ReadFile(vlan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v), "parent=bond0") || !strings.Contains(string(v), "id=80") {
		t.Fatalf("nm vlan: %s", v)
	}
	root2 := t.TempDir()
	if err := provision.ApplyNetwork(root2, cfg, "ifcfg"); err != nil {
		t.Fatal(err)
	}
	ifcfg, err := os.ReadFile(filepath.Join(root2, "etc/sysconfig/network-scripts/ifcfg-bond0.80"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ifcfg), "VLAN=yes") || !strings.Contains(string(ifcfg), "PHYSDEV=bond0") {
		t.Fatalf("ifcfg vlan: %s", ifcfg)
	}
}

func TestCloudInitKeys(t *testing.T) {
	user, meta := provision.CloudInit(model.InstallSpec{
		Hostname: "node-1", Username: "ops", SSHKeys: []string{"ssh-ed25519 AAAA demo"},
	}, "$6$hash")
	if !strings.Contains(user, "ssh-ed25519 AAAA demo") || !strings.Contains(user, "ops") {
		t.Fatalf("user-data: %s", user)
	}
	if !strings.Contains(user, "growpart") {
		t.Fatalf("missing growpart: %s", user)
	}
	if !strings.Contains(meta, "node-1") {
		t.Fatalf("meta: %s", meta)
	}
}
