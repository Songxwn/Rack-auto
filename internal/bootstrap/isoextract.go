package bootstrap

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const isoSector = 2048

type isoVol struct {
	rootLBA uint32
	rootLen uint32
	ucs2    bool
}

// ExtractISOFiles copies named files out of an ISO 9660 / Joliet image.
// Keys in want are relative paths such as "casper/vmlinuz" (case-insensitive).
func ExtractISOFiles(isoPath string, want map[string]string) error {
	if len(want) == 0 {
		return nil
	}
	f, err := os.Open(isoPath)
	if err != nil {
		return err
	}
	defer f.Close()
	vols, err := readISOVolumes(f)
	if err != nil {
		return err
	}
	missing := map[string]string{}
	for src, dst := range want {
		missing[src] = dst
	}
	for _, vol := range vols {
		for src, dst := range missing {
			lba, size, err := vol.find(f, src)
			if err != nil {
				continue
			}
			if err := copyISOExtent(f, lba, size, dst); err != nil {
				return fmt.Errorf("extract %s: %w", src, err)
			}
			delete(missing, src)
		}
		if len(missing) == 0 {
			return nil
		}
	}
	var names []string
	for src := range missing {
		names = append(names, src)
	}
	return fmt.Errorf("ISO 中找不到: %s", strings.Join(names, ", "))
}

func readISOVolumes(f *os.File) ([]isoVol, error) {
	var joliet, primary *isoVol
	for i := 16; i < 32; i++ {
		sec := make([]byte, isoSector)
		if _, err := f.ReadAt(sec, int64(i)*isoSector); err != nil {
			return nil, fmt.Errorf("读 ISO 卷描述符: %w", err)
		}
		if string(sec[1:6]) != "CD001" {
			continue
		}
		typ := sec[0]
		if typ == 255 {
			break
		}
		root := sec[156 : 156+34]
		if len(root) < 34 || root[0] < 34 {
			continue
		}
		v := isoVol{
			rootLBA: binary.LittleEndian.Uint32(root[2:6]),
			rootLen: binary.LittleEndian.Uint32(root[10:14]),
		}
		switch typ {
		case 1:
			cp := v
			primary = &cp
		case 2:
			esc := sec[88:91]
			if esc[0] == 0x25 && esc[1] == 0x2f && (esc[2] == 0x45 || esc[2] == 0x40 || esc[2] == 0x43) {
				v.ucs2 = true
				cp := v
				joliet = &cp
			}
		}
	}
	var out []isoVol
	if joliet != nil {
		out = append(out, *joliet)
	}
	if primary != nil {
		out = append(out, *primary)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("不是有效的 ISO 9660 映像")
	}
	return out, nil
}

func (v isoVol) find(f *os.File, path string) (uint32, uint32, error) {
	parts := splitISOPath(path)
	if len(parts) == 0 {
		return 0, 0, fmt.Errorf("empty path")
	}
	lba, size := v.rootLBA, v.rootLen
	for i, part := range parts {
		ents, err := v.readDir(f, lba, size)
		if err != nil {
			return 0, 0, err
		}
		ent, ok := lookupDir(ents, part)
		if !ok {
			return 0, 0, fmt.Errorf("%s: 没有 %s", path, part)
		}
		last := i == len(parts)-1
		if last {
			if ent.dir {
				return 0, 0, fmt.Errorf("%s 是目录", path)
			}
			return ent.lba, ent.size, nil
		}
		if !ent.dir {
			return 0, 0, fmt.Errorf("%s: %s 不是目录", path, part)
		}
		lba, size = ent.lba, ent.size
	}
	return 0, 0, fmt.Errorf("%s: not found", path)
}

type isoDirEnt struct {
	name string
	lba  uint32
	size uint32
	dir  bool
}

func (v isoVol) readDir(f *os.File, lba, size uint32) ([]isoDirEnt, error) {
	if size == 0 {
		return nil, fmt.Errorf("empty directory")
	}
	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, int64(lba)*isoSector); err != nil {
		return nil, err
	}
	var out []isoDirEnt
	off := 0
	for off < len(buf) {
		if buf[off] == 0 {
			next := ((off / isoSector) + 1) * isoSector
			if next <= off {
				break
			}
			off = next
			continue
		}
		ent, n, ok := parseDirRecord(buf[off:], v.ucs2)
		if !ok || n <= 0 {
			break
		}
		if ent.name != "" && ent.name != "." && ent.name != ".." {
			out = append(out, ent)
		}
		off += n
	}
	return out, nil
}

func parseDirRecord(b []byte, ucs2 bool) (isoDirEnt, int, bool) {
	if len(b) < 34 {
		return isoDirEnt{}, 0, false
	}
	recLen := int(b[0])
	if recLen < 34 || recLen > len(b) {
		return isoDirEnt{}, 0, false
	}
	nameLen := int(b[32])
	if nameLen < 0 || 33+nameLen > recLen {
		return isoDirEnt{}, recLen, false
	}
	name := decodeISOName(b[33:33+nameLen], ucs2)
	return isoDirEnt{
		name: name,
		lba:  binary.LittleEndian.Uint32(b[2:6]),
		size: binary.LittleEndian.Uint32(b[10:14]),
		dir:  b[25]&0x02 != 0,
	}, recLen, true
}

func decodeISOName(raw []byte, ucs2 bool) string {
	if len(raw) == 1 && raw[0] == 0 {
		return "."
	}
	if len(raw) == 1 && raw[0] == 1 {
		return ".."
	}
	var s string
	if ucs2 {
		var b strings.Builder
		for i := 0; i+1 < len(raw); i += 2 {
			r := rune(binary.BigEndian.Uint16(raw[i : i+2]))
			if r == 0 {
				continue
			}
			b.WriteRune(r)
		}
		s = b.String()
	} else {
		s = string(raw)
	}
	return normalizeISOName(s)
}

func normalizeISOName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ";"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, ".")
	return strings.ToLower(strings.TrimSpace(s))
}

func splitISOPath(path string) []string {
	path = strings.ReplaceAll(path, "\\", "/")
	var parts []string
	for _, p := range strings.Split(path, "/") {
		p = normalizeISOName(p)
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	return parts
}

func lookupDir(ents []isoDirEnt, name string) (isoDirEnt, bool) {
	name = normalizeISOName(name)
	for _, e := range ents {
		if e.name == name {
			return e, true
		}
	}
	return isoDirEnt{}, false
}

func copyISOExtent(f *os.File, lba, size uint32, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	r := io.NewSectionReader(f, int64(lba)*isoSector, int64(size))
	_, err = io.Copy(out, r)
	cerr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	return os.Rename(tmp, dest)
}
