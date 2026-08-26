package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/model"
)

var dmiDir = "/sys/class/dmi/id"

func applyDMI(inv *model.Inventory) {
	if inv == nil {
		return
	}
	inv.Vendor = readDMI("sys_vendor")
	inv.Product = readDMI("product_name")
	inv.ProductVersion = readDMI("product_version")
	inv.Family = readDMI("product_family")
	inv.SKU = readDMI("product_sku")
	inv.UUID = readDMI("product_uuid")
	inv.Serial = firstDMI("product_serial", "chassis_serial")
	inv.BoardVendor = readDMI("board_vendor")
	inv.BoardName = readDMI("board_name")
	inv.BoardSerial = readDMI("board_serial")
	inv.AssetTag = firstDMI("chassis_asset_tag", "product_asset_tag")
	inv.BIOSVendor = readDMI("bios_vendor")
	inv.BIOSVersion = readDMI("bios_version")
	inv.BIOSDate = readDMI("bios_date")
	if inv.HasIdentity() || inv.BIOSVersion != "" {
		inv.DetectSource = "dmi"
	}
}

func firstDMI(names ...string) string {
	for _, n := range names {
		if v := readDMI(n); v != "" {
			return v
		}
	}
	return ""
}

func readDMI(name string) string {
	b, err := os.ReadFile(filepath.Join(dmiDir, name))
	if err != nil {
		return ""
	}
	return sanitizeDMI(string(b))
}

func sanitizeDMI(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	switch strings.ToLower(s) {
	case "none", "not specified", "not available", "not set", "unknown",
		"to be filled by o.e.m.", "to be filled by oem", "default string",
		"system serial number", "chassis serial number", "system product name",
		"system manufacturer", "na", "n/a", "null", "0", "0123456789", "empty":
		return ""
	}
	return s
}
