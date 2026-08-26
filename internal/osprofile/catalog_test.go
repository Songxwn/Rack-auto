package osprofile

import "testing"

func TestLookupBackends(t *testing.T) {
	cases := []struct {
		family, ver, net, fs, user string
	}{
		{"ubuntu", "24.04", Netplan, "ext4", "ubuntu"},
		{"debian", "12", Ifupdown, "ext4", "debian"},
		{"debian", "13", Netplan, "ext4", "debian"},
		{"rocky", "8", Ifcfg, "xfs", "rocky"},
		{"rocky", "9", NM, "xfs", "rocky"},
		{"alma", "9", NM, "xfs", "almalinux"},
		{"centos", "7", Ifcfg, "ext4", "centos"},
		{"", "", Netplan, "ext4", "ubuntu"},
	}
	for _, c := range cases {
		v := Lookup(c.family, c.ver)
		if v.NetBackend != c.net || v.RootFS != c.fs || v.DefaultUser != c.user {
			t.Fatalf("%s %s: %+v", c.family, c.ver, v)
		}
	}
}

func TestLabel(t *testing.T) {
	if got := Label("debian", "12"); got != "Debian 12 (bookworm)" {
		t.Fatal(got)
	}
	if got := Label("rocky linux", "9"); got != "Rocky Linux 9" {
		t.Fatal(got)
	}
}
