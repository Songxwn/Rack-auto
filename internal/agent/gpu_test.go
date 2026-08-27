package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePCIIdsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pci.ids")
	content := `# comment
10de  NVIDIA Corporation
	2204  GA102 [GeForce RTX 3090]
	1b06  GP102 [GeForce GTX 1080 Ti]
1002  Advanced Micro Devices, Inc. [AMD/ATI]
	73bf  Navi 21 [Radeon RX 6800/6800 XT / 6900 XT]
		0732  Radeon RX 6800 XT
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	db := pciIdsDB{vendors: map[uint16]string{}, devices: map[uint32]string{}}
	if !parsePCIIdsFile(path, &db) {
		t.Fatal("parse failed")
	}
	if db.vendors[0x10de] != "NVIDIA Corporation" {
		t.Fatalf("vendor: %q", db.vendors[0x10de])
	}
	v, d := db.lookup(0x10de, 0x2204)
	if v != "NVIDIA Corporation" || d != "GA102 [GeForce RTX 3090]" {
		t.Fatalf("lookup got %q %q", v, d)
	}
	_, d2 := db.lookup(0x1002, 0x73bf)
	if d2 != "Navi 21 [Radeon RX 6800/6800 XT / 6900 XT]" {
		t.Fatalf("amd device: %q", d2)
	}
}

func TestFormatPCIID(t *testing.T) {
	if got := formatPCIID(0x10de, 0x2204); got != "10de:2204" {
		t.Fatal(got)
	}
	if got := formatPCIID(0x8086, 0xa780); got != "8086:a780" {
		t.Fatal(got)
	}
}

func TestPreferOper(t *testing.T) {
	if preferOper("dormant", "up") != "dormant" {
		t.Fatal("keep got")
	}
	if preferOper("", "down") != "down" {
		t.Fatal("fallback")
	}
}
