package provision_test

import (
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func TestDefaultPartitions(t *testing.T) {
	uefi := provision.DefaultPartitions(model.FirmwareUEFI)
	if err := provision.Validate(uefi); err != nil {
		t.Fatal(err)
	}
	bios := provision.DefaultPartitions(model.FirmwareBIOS)
	if err := provision.Validate(bios); err != nil {
		t.Fatal(err)
	}
}

func TestSGDiskAndFstab(t *testing.T) {
	parts := provision.DefaultPartitions(model.FirmwareUEFI)
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

func TestCloudInitKeys(t *testing.T) {
	user, meta := provision.CloudInit(model.InstallSpec{
		Hostname: "node-1", Username: "ops", SSHKeys: []string{"ssh-ed25519 AAAA demo"},
	}, "$6$hash")
	if !strings.Contains(user, "ssh-ed25519 AAAA demo") || !strings.Contains(user, "ops") {
		t.Fatalf("user-data: %s", user)
	}
	if !strings.Contains(meta, "node-1") {
		t.Fatalf("meta: %s", meta)
	}
}
