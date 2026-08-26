package model

import "testing"

func TestProductLine(t *testing.T) {
	inv := &Inventory{Vendor: "Dell Inc.", Product: "PowerEdge R640", Serial: "ABC"}
	if got := inv.ProductLine(); got != "Dell Inc. PowerEdge R640" {
		t.Fatal(got)
	}
	inv.Product = "Dell Inc. PowerEdge R640"
	if got := inv.ProductLine(); got != "Dell Inc. PowerEdge R640" {
		t.Fatal(got)
	}
	if inv.IdentityName() == "" || !inv.HasIdentity() {
		t.Fatal("identity")
	}
}

func TestIdentityNameModelSerial(t *testing.T) {
	inv := &Inventory{Vendor: "Dell Inc.", Product: "PowerEdge R640", Serial: "4ABC123"}
	if got := inv.IdentityName(); got != "PowerEdge R640 4ABC123" {
		t.Fatalf("got %q", got)
	}
	inv.Product = "Dell Inc. PowerEdge R640"
	if got := inv.IdentityName(); got != "PowerEdge R640 4ABC123" {
		t.Fatalf("vendor prefix: %q", got)
	}
	inv.Serial = ""
	if got := inv.IdentityName(); got != "PowerEdge R640" {
		t.Fatalf("model only: %q", got)
	}
	inv.Product = ""
	inv.Serial = "XYZ"
	if got := inv.IdentityName(); got != "XYZ" {
		t.Fatalf("serial only: %q", got)
	}
}

func TestIsLiveHostname(t *testing.T) {
	for _, n := range []string{"ubuntu", "Ubuntu", "ubuntu.localdomain", "ramos", "minwinpc", "MININT-ABC"} {
		if !IsLiveHostname(n) {
			t.Fatalf("%q should be live hostname", n)
		}
	}
	if IsLiveHostname("web-01") || IsLiveHostname("PowerEdge R640 4ABC123") {
		t.Fatal("custom names are not live hostnames")
	}
}
