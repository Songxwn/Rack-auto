package osprofile

import "testing"

func TestLookupBackends(t *testing.T) {
	cases := []struct {
		family, ver, net, fs, user string
	}{
		{"ubuntu", "24.04", Netplan, "ext4", "root"},
		{"debian", "12", Ifupdown, "ext4", "root"},
		{"debian", "13", Netplan, "ext4", "root"},
		{"rocky", "8", Ifcfg, "xfs", "root"},
		{"rocky", "9", NM, "xfs", "root"},
		{"alma", "8", Ifcfg, "xfs", "root"},
		{"alma", "9", NM, "xfs", "root"},
		{"alma", "10", NM, "xfs", "root"},
		{"centos", "7", Ifcfg, "ext4", "root"},
		{"centos", "8", Ifcfg, "xfs", "root"},
		{"centos stream", "9", NM, "xfs", "root"},
		{"centos", "10", NM, "xfs", "root"},
		{"windows", "2022", Windows, "ntfs", "Administrator"},
		{"windows server", "2019", Windows, "ntfs", "Administrator"},
		{"", "", Netplan, "ext4", "root"},
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
	if got := Label("almalinux", "10"); got != "AlmaLinux 10" {
		t.Fatal(got)
	}
	if got := Label("centos stream", "8"); got != "CentOS Stream 8" {
		t.Fatal(got)
	}
	if got := Label("windows", "2025"); got != "Windows Server 2025" {
		t.Fatal(got)
	}
	if !IsWindows("Windows Server") {
		t.Fatal("IsWindows")
	}
}
