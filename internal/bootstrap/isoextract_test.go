package bootstrap

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractISOFiles(t *testing.T) {
	iso := writeTestISO(t, map[string][]byte{
		"casper/vmlinuz": []byte("kernel-bytes-here"),
		"casper/initrd":  []byte("initrd-bytes-here"),
	})
	dir := t.TempDir()
	vmlinuz := filepath.Join(dir, "vmlinuz")
	initrd := filepath.Join(dir, "initrd")
	if err := ExtractISOFiles(iso, map[string]string{
		"casper/vmlinuz": vmlinuz,
		"casper/initrd":  initrd,
	}); err != nil {
		t.Fatal(err)
	}
	off, sz, err := ISOFileLocation(iso, "casper/vmlinuz")
	if err != nil || sz != int64(len("kernel-bytes-here")) || off <= 0 {
		t.Fatalf("location off=%d sz=%d err=%v", off, sz, err)
	}
	got, _ := os.ReadFile(vmlinuz)
	if string(got) != "kernel-bytes-here" {
		t.Fatalf("vmlinuz %q", got)
	}
	got, _ = os.ReadFile(initrd)
	if string(got) != "initrd-bytes-here" {
		t.Fatalf("initrd %q", got)
	}
}

func TestNormalizeISOName(t *testing.T) {
	if normalizeISOName("VMLINUZ.;1") != "vmlinuz" {
		t.Fatal(normalizeISOName("VMLINUZ.;1"))
	}
	if normalizeISOName("casper") != "casper" {
		t.Fatal(normalizeISOName("casper"))
	}
}

func writeTestISO(t *testing.T, files map[string][]byte) string {
	t.Helper()
	type blob struct {
		name string
		data []byte
		lba  uint32
	}
	var blobs []blob
	next := uint32(20)
	for name, data := range files {
		base := name
		if i := len(name) - 1; i >= 0 {
			for j := len(name) - 1; j >= 0; j-- {
				if name[j] == '/' {
					base = name[j+1:]
					break
				}
			}
		}
		nsec := uint32((len(data) + isoSector - 1) / isoSector)
		if nsec == 0 {
			nsec = 1
		}
		blobs = append(blobs, blob{name: base, data: data, lba: next})
		next += nsec
	}

	const (
		pvdLBA    = 16
		termLBA   = 17
		rootLBA   = 18
		casperLBA = 19
	)
	rootSec := make([]byte, isoSector)
	off := 0
	off += putDirRec(rootSec[off:], []byte{0}, rootLBA, isoSector, true)
	off += putDirRec(rootSec[off:], []byte{1}, rootLBA, isoSector, true)
	_ = putDirRec(rootSec[off:], []byte("CASPER"), casperLBA, isoSector, true)

	casperSec := make([]byte, isoSector)
	off = 0
	off += putDirRec(casperSec[off:], []byte{0}, casperLBA, isoSector, true)
	off += putDirRec(casperSec[off:], []byte{1}, rootLBA, isoSector, true)
	for _, b := range blobs {
		nm := []byte(toISO9660Name(b.name))
		off += putDirRec(casperSec[off:], nm, b.lba, uint32(len(b.data)), false)
	}

	pvd := make([]byte, isoSector)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[40:72], []byte("TESTISO                         "))
	putBoth32(pvd[80:88], next)
	putBoth16(pvd[128:132], isoSector)
	putDirRec(pvd[156:], []byte{0}, rootLBA, isoSector, true)

	term := make([]byte, isoSector)
	term[0] = 255
	copy(term[1:6], "CD001")
	term[6] = 1

	total := int64(next) * isoSector
	buf := make([]byte, total)
	copy(buf[pvdLBA*isoSector:], pvd)
	copy(buf[termLBA*isoSector:], term)
	copy(buf[rootLBA*isoSector:], rootSec)
	copy(buf[casperLBA*isoSector:], casperSec)
	for _, b := range blobs {
		copy(buf[int(b.lba)*isoSector:], b.data)
	}

	path := filepath.Join(t.TempDir(), "test.iso")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func toISO9660Name(name string) string {
	b := []byte(name)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func putBoth32(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b[0:4], v)
	binary.BigEndian.PutUint32(b[4:8], v)
}

func putBoth16(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b[0:2], v)
	binary.BigEndian.PutUint16(b[2:4], v)
}

func putDirRec(dst []byte, name []byte, lba, size uint32, dir bool) int {
	nameLen := len(name)
	recLen := 33 + nameLen
	if recLen%2 == 1 {
		recLen++
	}
	if recLen > len(dst) {
		panic("dir sector full")
	}
	dst[0] = byte(recLen)
	putBoth32(dst[2:10], lba)
	putBoth32(dst[10:18], size)
	if dir {
		dst[25] = 0x02
	}
	dst[28] = 1
	dst[30] = 1
	dst[32] = byte(nameLen)
	copy(dst[33:], name)
	return recLen
}
