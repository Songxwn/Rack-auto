package server

import (
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func TestAutoMachineName(t *testing.T) {
	inv := &model.Inventory{Vendor: "Dell Inc.", Product: "PowerEdge R640", Serial: "4ABC123"}
	mac := "aa:bb:cc:dd:ee:ff"
	if got := autoMachineName("ubuntu", mac, inv); got != "PowerEdge R640 4ABC123" {
		t.Fatalf("live hostname: %q", got)
	}
	if got := autoMachineName("web-01", mac, inv); got != "web-01" {
		t.Fatalf("custom name: %q", got)
	}
	if got := autoMachineName("ubuntu", mac, nil); got != mac {
		t.Fatalf("no inventory: %q", got)
	}
	if got := autoMachineName("PowerEdge R640", mac, inv); got != "PowerEdge R640 4ABC123" {
		t.Fatalf("model only: %q", got)
	}
}
