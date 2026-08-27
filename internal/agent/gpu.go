package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Songxwn/Rack-auto/internal/model"
)

// listGPUs scans PCI display controllers (class 0x03xxxx) via sysfs.
func listGPUs() []model.GPU {
	entries, err := os.ReadDir("/sys/bus/pci/devices")
	if err != nil {
		return nil
	}
	ids := loadPCIIds()
	var out []model.GPU
	for _, e := range entries {
		bus := e.Name()
		base := filepath.Join("/sys/bus/pci/devices", bus)
		class := readSysHex(filepath.Join(base, "class"))
		if class>>16 != 0x03 {
			continue
		}
		vendor := uint16(readSysHex(filepath.Join(base, "vendor")))
		device := uint16(readSysHex(filepath.Join(base, "device")))
		if vendor == 0 && device == 0 {
			continue
		}
		pciID := formatPCIID(vendor, device)
		vName, dName := ids.lookup(vendor, device)
		if vName == "" {
			vName = knownPCIVendor(vendor)
		}
		modelName := dName
		if modelName == "" {
			modelName = pciID
		}
		driver := ""
		if link, err := os.Readlink(filepath.Join(base, "driver")); err == nil {
			driver = filepath.Base(link)
		}
		out = append(out, model.GPU{
			Index:  len(out),
			Vendor: vName,
			Model:  modelName,
			PCIID:  pciID,
			Driver: driver,
			Bus:    bus,
		})
	}
	return out
}

func formatPCIID(vendor, device uint16) string {
	return strings.ToLower(pad4(vendor) + ":" + pad4(device))
}

func pad4(v uint16) string {
	s := strconv.FormatUint(uint64(v), 16)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func readSysHex(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(b))
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	n, _ := strconv.ParseUint(s, 16, 64)
	return n
}

func knownPCIVendor(id uint16) string {
	switch id {
	case 0x10de:
		return "NVIDIA Corporation"
	case 0x1002:
		return "Advanced Micro Devices, Inc. [AMD/ATI]"
	case 0x8086:
		return "Intel Corporation"
	case 0x1a03:
		return "ASPEED Technology, Inc."
	case 0x15ad:
		return "VMware"
	case 0x1234:
		return "QEMU"
	case 0x1af4:
		return "Red Hat, Inc."
	case 0x1414:
		return "Microsoft Corporation"
	default:
		return ""
	}
}

type pciIdsDB struct {
	vendors map[uint16]string
	devices map[uint32]string // vendor<<16 | device
}

var (
	pciIdsOnce sync.Once
	pciIds     pciIdsDB
)

func loadPCIIds() pciIdsDB {
	pciIdsOnce.Do(func() {
		pciIds.vendors = map[uint16]string{}
		pciIds.devices = map[uint32]string{}
		for _, p := range []string{
			"/usr/share/misc/pci.ids",
			"/usr/share/hwdata/pci.ids",
			"/usr/share/pci.ids",
		} {
			if parsePCIIdsFile(p, &pciIds) {
				break
			}
		}
	})
	return pciIds
}

func parsePCIIdsFile(path string, db *pciIdsDB) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var curVendor uint16
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// device line: single tab
		if strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "\t\t") {
			rest := strings.TrimPrefix(line, "\t")
			idStr, name, ok := strings.Cut(rest, "  ")
			if !ok {
				continue
			}
			id, err := strconv.ParseUint(strings.TrimSpace(idStr), 16, 16)
			if err != nil || curVendor == 0 {
				continue
			}
			db.devices[uint32(curVendor)<<16|uint32(id)] = strings.TrimSpace(name)
			continue
		}
		if strings.HasPrefix(line, "\t") {
			continue // subsystem lines
		}
		idStr, name, ok := strings.Cut(line, "  ")
		if !ok {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(idStr), 16, 16)
		if err != nil {
			continue
		}
		curVendor = uint16(id)
		db.vendors[curVendor] = strings.TrimSpace(name)
	}
	return len(db.vendors) > 0
}

func (db pciIdsDB) lookup(vendor, device uint16) (vendorName, deviceName string) {
	if db.vendors != nil {
		vendorName = db.vendors[vendor]
	}
	if db.devices != nil {
		deviceName = db.devices[uint32(vendor)<<16|uint32(device)]
	}
	return vendorName, deviceName
}

// nicLinkState prefers sysfs carrier/operstate over IFF_UP (admin flag).
func nicLinkState(name string, flagUp bool) (up bool, oper string) {
	base := filepath.Join("/sys/class/net", name)
	if b, err := os.ReadFile(filepath.Join(base, "operstate")); err == nil {
		oper = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(filepath.Join(base, "carrier")); err == nil {
		switch strings.TrimSpace(string(b)) {
		case "1":
			return true, preferOper(oper, "up")
		case "0":
			return false, preferOper(oper, "down")
		}
	}
	switch oper {
	case "up":
		return true, oper
	case "down", "lowerlayerdown", "notpresent":
		return false, oper
	}
	return flagUp, oper
}

func preferOper(got, fallback string) string {
	if got != "" {
		return got
	}
	return fallback
}
