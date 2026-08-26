package bmc

import (
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func TestParseRedfishSystem(t *testing.T) {
	inv := parseRedfishSystem([]byte(`{
		"Manufacturer": "Dell Inc.",
		"Model": "PowerEdge R640",
		"SerialNumber": "ABC123",
		"SKU": "SKU-9",
		"UUID": "1234-5678",
		"BiosVersion": "2.19.0",
		"HostName": "node-1"
	}`))
	if inv.Vendor != "Dell Inc." || inv.Product != "PowerEdge R640" || inv.Serial != "ABC123" {
		t.Fatalf("%+v", inv)
	}
	dst := &model.Inventory{CPUs: 16, Disks: []model.Disk{{Path: "/dev/sda"}}}
	MergeIdentity(dst, inv)
	if dst.Vendor != "Dell Inc." || dst.CPUs != 16 || len(dst.Disks) != 1 {
		t.Fatalf("merge: %+v", dst)
	}
}
