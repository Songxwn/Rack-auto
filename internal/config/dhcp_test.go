package config

import "testing"

func TestValidateDHCPRequiresInterface(t *testing.T) {
	d := DHCP{Enabled: true, RangeStart: "10.0.0.10", RangeEnd: "10.0.0.20", Subnet: "10.0.0.0/24"}
	if err := d.Validate(); err == nil {
		t.Fatal("expected interface error")
	}
	d.Interface = "eth1"
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDHCPDisabled(t *testing.T) {
	if err := (DHCP{Enabled: false}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSuggestPool24(t *testing.T) {
	s, e := SuggestPool("10.10.1.0/24")
	if s != "10.10.1.100" || e != "10.10.1.200" {
		t.Fatalf("%s %s", s, e)
	}
}

func TestPoolOutsideSubnet(t *testing.T) {
	d := DHCP{Enabled: true, Interface: "eth0", Subnet: "10.0.0.0/24", RangeStart: "192.168.1.10", RangeEnd: "192.168.1.20"}
	if err := d.Validate(); err == nil {
		t.Fatal("expected subnet error")
	}
}
