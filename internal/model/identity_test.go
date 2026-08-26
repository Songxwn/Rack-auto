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
