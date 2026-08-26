package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func TestSanitizeDMI(t *testing.T) {
	if sanitizeDMI("  Dell Inc.  ") != "Dell Inc." {
		t.Fatal("trim")
	}
	if sanitizeDMI("To Be Filled By O.E.M.") != "" {
		t.Fatal("placeholder")
	}
	if sanitizeDMI("None") != "" {
		t.Fatal("none")
	}
}

func TestApplyDMI(t *testing.T) {
	dir := t.TempDir()
	write := func(name, val string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(val+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("sys_vendor", "Dell Inc.")
	write("product_name", "PowerEdge R640")
	write("product_serial", "To Be Filled By O.E.M.")
	write("chassis_serial", "ABC123")
	write("bios_version", "2.19.0")
	old := dmiDir
	dmiDir = dir
	defer func() { dmiDir = old }()
	inv := &model.Inventory{}
	applyDMI(inv)
	if inv.Vendor != "Dell Inc." || inv.Product != "PowerEdge R640" || inv.Serial != "ABC123" {
		t.Fatalf("%+v", inv)
	}
	if inv.ProductLine() != "Dell Inc. PowerEdge R640" {
		t.Fatal(inv.ProductLine())
	}
	if inv.DetectSource != "dmi" {
		t.Fatal(inv.DetectSource)
	}
}
