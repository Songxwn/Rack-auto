package imageinspect

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Songxwn/Rack-auto/internal/model"
	qcow2reader "github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
)

var (
	guidESP   = guidFrom("C12A7328-F81F-11D2-BA4B-00A0C93EC93B")
	guidBIOS  = guidFrom("21686148-6449-6E6F-744E-656564454649")
	guidLinux = guidFrom("0FC63DAF-8483-4772-8E79-3D69D8477DE4")
	guidSwap  = guidFrom("0657FD6D-A4AB-43C4-84E5-0933C84B4F4F")
)

var efi83Names = [][]byte{
	[]byte("BOOTX64 EFI"),
	[]byte("BOOTAA64EFI"),
	[]byte("GRUBX64 EFI"),
	[]byte("GRUBAA64EFI"),
}

type disk struct {
	r    io.ReaderAt
	size int64
}

func File(path string) *model.ImageInspect {
	out := &model.ImageInspect{InspectedAt: time.Now().UTC(), Status: "error"}
	f, err := os.Open(path)
	if err != nil {
		out.Message = err.Error()
		return out
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		out.Message = err.Error()
		return out
	}
	hdr := make([]byte, 8)
	_, _ = f.ReadAt(hdr, 0)
	var r io.ReaderAt = f
	size := st.Size()
	format := "raw"
	if string(hdr[:4]) == "QFI\xfb" {
		img, err := qcow2reader.OpenWithType(f, qcow2.Type)
		if err != nil {
			out.Message = "open qcow2: " + err.Error()
			return out
		}
		defer img.Close()
		if err := img.Readable(); err != nil {
			out.Message = "qcow2 not readable: " + err.Error()
			return out
		}
		r = img
		size = img.Size()
		format = "qcow2"
	} else if string(hdr) == wimMagic {
		got := inspectWIMFile(size, f)
		got.InspectedAt = time.Now().UTC()
		return got
	} else if win := inspectWindowsISO(size, f); win != nil {
		return win
	}
	return Inspect(disk{r: r, size: size}, format)
}

func Inspect(d disk, format string) *model.ImageInspect {
	out := &model.ImageInspect{
		Status:       "ok",
		Format:       format,
		VirtualSizeB: d.size,
		InspectedAt:  time.Now().UTC(),
	}
	if d.size < 1024 {
		out.Status = "error"
		out.Message = "file too small to be a disk image"
		return out
	}
	sec := detectSector(d.r)
	parts, table := parseTable(d.r, d.size, sec)
	out.Table = table
	out.Partitions = parts
	if table == "none" {
		if fs := ProbeFS(d.r, 0); fs != "" {
			out.RootFS = fs
			out.RootNum = 0
			out.Message = "single filesystem image; use kind cloud-root and install a bootloader"
			return out
		}
		out.Status = "error"
		out.Message = "no partition table and no Linux filesystem at start"
		return out
	}
	var warns []string
	var bestLinux model.InspectPartition
	for i := range parts {
		p := &parts[i]
		if p.SizeB <= 0 {
			continue
		}
		fs := ProbeFS(d.r, p.StartB)
		p.FS = fs
		switch p.Type {
		case "esp":
			out.ESPNum = p.Number
			out.BootUEFI = true
			if name := scanEFI(d.r, p.StartB, p.SizeB); name != "" {
				out.EFILoader = name
			} else {
				warns = append(warns, "ESP found but EFI loader not verified")
			}
		case "bios_grub":
			out.BootBIOS = true
		}
		if fs == "ext4" || fs == "xfs" || fs == "btrfs" {
			if p.SizeB >= bestLinux.SizeB {
				bestLinux = *p
			}
		}
	}
	if bestLinux.Number != 0 {
		out.RootFS = bestLinux.FS
		out.RootNum = bestLinux.Number
	}
	if table == "mbr" && mbrActive(d.r) {
		out.BootBIOS = true
	}
	if out.BootUEFI && out.EFILoader == "" && out.ESPNum != 0 {
		// still UEFI-capable via NVRAM/fallback path
	}
	if out.RootFS == "" {
		out.Status = "warn"
		out.Message = "no Linux root filesystem detected"
	} else if !out.BootUEFI && !out.BootBIOS {
		out.Status = "warn"
		out.Message = "Linux root found but no UEFI ESP or BIOS boot partition"
	} else {
		out.Message = bootSummary(out)
	}
	out.Warnings = warns
	out.Partitions = parts
	return out
}

func bootSummary(in *model.ImageInspect) string {
	var modes []string
	if in.BootUEFI {
		if in.EFILoader != "" {
			modes = append(modes, "UEFI ("+in.EFILoader+")")
		} else {
			modes = append(modes, "UEFI")
		}
	}
	if in.BootBIOS {
		modes = append(modes, "BIOS")
	}
	msg := "bootable: "
	for i, m := range modes {
		if i > 0 {
			msg += " / "
		}
		msg += m
	}
	if in.RootFS != "" {
		msg += "; root " + in.RootFS
		if in.RootNum > 0 {
			msg += fmt.Sprintf(" p%d", in.RootNum)
		}
	}
	return msg
}

func detectSector(r io.ReaderAt) int64 {
	b := make([]byte, 8)
	if _, err := r.ReadAt(b, 512); err == nil && string(b[:8]) == "EFI PART" {
		return 512
	}
	if _, err := r.ReadAt(b, 4096); err == nil && string(b[:8]) == "EFI PART" {
		return 4096
	}
	return 512
}

func parseTable(r io.ReaderAt, size, sec int64) ([]model.InspectPartition, string) {
	if parts, ok := parseGPT(r, sec); ok {
		return parts, "gpt"
	}
	if parts, ok := parseMBR(r, size, sec); ok {
		return parts, "mbr"
	}
	return nil, "none"
}

func parseGPT(r io.ReaderAt, sec int64) ([]model.InspectPartition, bool) {
	hdr := make([]byte, sec)
	if _, err := r.ReadAt(hdr, sec); err != nil || string(hdr[:8]) != "EFI PART" {
		return nil, false
	}
	entryLBA := binary.LittleEndian.Uint64(hdr[72:80])
	nEnt := binary.LittleEndian.Uint32(hdr[80:84])
	entSize := binary.LittleEndian.Uint32(hdr[84:88])
	if entSize < 128 || nEnt == 0 || nEnt > 4096 {
		return nil, false
	}
	need := int(nEnt) * int(entSize)
	buf := make([]byte, need)
	if _, err := r.ReadAt(buf, int64(entryLBA)*sec); err != nil {
		return nil, false
	}
	var out []model.InspectPartition
	for i := 0; i < int(nEnt); i++ {
		e := buf[i*int(entSize) : i*int(entSize)+128]
		if isZero(e[:16]) {
			continue
		}
		first := binary.LittleEndian.Uint64(e[32:40])
		last := binary.LittleEndian.Uint64(e[40:48])
		if last < first {
			continue
		}
		p := model.InspectPartition{
			Number: i + 1,
			Type:   gptType(e[:16]),
			StartB: int64(first) * sec,
			SizeB:  int64(last-first+1) * sec,
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func parseMBR(r io.ReaderAt, size, sec int64) ([]model.InspectPartition, bool) {
	b := make([]byte, 512)
	if _, err := r.ReadAt(b, 0); err != nil {
		return nil, false
	}
	if b[510] != 0x55 || b[511] != 0xAA {
		return nil, false
	}
	var out []model.InspectPartition
	for i := 0; i < 4; i++ {
		e := b[446+i*16 : 446+i*16+16]
		typ := e[4]
		if typ == 0 || typ == 0xEE {
			continue
		}
		start := binary.LittleEndian.Uint32(e[8:12])
		n := binary.LittleEndian.Uint32(e[12:16])
		if n == 0 {
			continue
		}
		p := model.InspectPartition{
			Number: i + 1,
			Type:   mbrType(typ),
			StartB: int64(start) * sec,
			SizeB:  int64(n) * sec,
		}
		if p.StartB+p.SizeB > size && size > 0 {
			p.SizeB = size - p.StartB
		}
		out = append(out, p)
	}
	return out, len(out) > 0
}

func mbrActive(r io.ReaderAt) bool {
	b := make([]byte, 512)
	if _, err := r.ReadAt(b, 0); err != nil {
		return false
	}
	if b[510] != 0x55 || b[511] != 0xAA {
		return false
	}
	for i := 0; i < 4; i++ {
		if b[446+i*16] == 0x80 && b[446+i*16+4] != 0 && b[446+i*16+4] != 0xEE {
			return true
		}
	}
	return false
}

func gptType(g []byte) string {
	switch {
	case bytes.Equal(g, guidESP[:]):
		return "esp"
	case bytes.Equal(g, guidBIOS[:]):
		return "bios_grub"
	case bytes.Equal(g, guidLinux[:]):
		return "linux"
	case bytes.Equal(g, guidSwap[:]):
		return "swap"
	default:
		return "other"
	}
}

func mbrType(t byte) string {
	switch t {
	case 0xEF:
		return "esp"
	case 0x83:
		return "linux"
	case 0x82:
		return "swap"
	default:
		return "other"
	}
}

func ProbeFS(r io.ReaderAt, start int64) string {
	buf := make([]byte, 2048)
	if _, err := r.ReadAt(buf, start); err != nil && err != io.EOF {
		return ""
	}
	if len(buf) >= 1024+0x3A && buf[1024+0x38] == 0x53 && buf[1024+0x39] == 0xEF {
		return "ext4"
	}
	if string(buf[:4]) == "XFSB" {
		return "xfs"
	}
	if string(buf[:8]) == "_BHRfS_M" {
		return "btrfs"
	}
	if buf[510] == 0x55 && buf[511] == 0xAA {
		if buf[0] == 0xEB || buf[0] == 0xE9 {
			if string(buf[82:90]) == "FAT32   " || string(buf[54:62]) == "FAT16   " || string(buf[54:62]) == "FAT12   " {
				return "vfat"
			}
			if buf[82] == 'F' && buf[83] == 'A' && buf[84] == 'T' {
				return "vfat"
			}
			if buf[54] == 'F' && buf[55] == 'A' && buf[56] == 'T' {
				return "vfat"
			}
		}
	}
	return ""
}

func scanEFI(r io.ReaderAt, start, size int64) string {
	n := size
	if n > 4<<20 {
		n = 4 << 20
	}
	if n < 512 {
		return ""
	}
	buf := make([]byte, int(n))
	got, err := r.ReadAt(buf, start)
	if got == 0 {
		return ""
	}
	_ = err
	buf = buf[:got]
	for _, name := range efi83Names {
		if bytes.Contains(buf, name) {
			nm := string(bytes.TrimSpace(name[:8]))
			ext := string(bytes.TrimSpace(name[8:]))
			return nm + "." + ext
		}
	}
	return ""
}

func guidFrom(s string) [16]byte {
	var raw [16]byte
	var d1 uint32
	var d2, d3 uint16
	var d4 [8]byte
	fmt.Sscanf(s, "%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		&d1, &d2, &d3, &d4[0], &d4[1], &d4[2], &d4[3], &d4[4], &d4[5], &d4[6], &d4[7])
	binary.LittleEndian.PutUint32(raw[0:4], d1)
	binary.LittleEndian.PutUint16(raw[4:6], d2)
	binary.LittleEndian.PutUint16(raw[6:8], d3)
	copy(raw[8:], d4[:])
	return raw
}

func isZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
