package bootstrap

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

type isoSrcFile struct {
	rel  string
	src  string
	size int64
	lba  uint32
}

func packCasperISO(srcDir, dest string) error {
	if err := packCasperISOExternal(srcDir, dest); err == nil {
		return nil
	}
	fmt.Println("   xorriso/genisoimage not found; packing casper.iso with built-in Joliet")
	return packCasperISOGo(srcDir, dest)
}

func packCasperISOExternal(srcDir, dest string) error {
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	cmds := [][]string{
		{"xorriso", "-as", "mkisofs", "-quiet", "-J", "-r", "-V", "RAMOS", "-o", tmp, srcDir},
		{"genisoimage", "-quiet", "-J", "-r", "-V", "RAMOS", "-o", tmp, srcDir},
		{"mkisofs", "-quiet", "-J", "-r", "-V", "RAMOS", "-o", tmp, srcDir},
	}
	var last error
	for _, args := range cmds {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			last = fmt.Errorf("%s: %w (%s)", args[0], err, strings.TrimSpace(string(out)))
			_ = os.Remove(tmp)
			continue
		}
		return os.Rename(tmp, dest)
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("no mkisofs")
}

func packCasperISOGo(srcDir, dest string) error {
	var files []isoSrcFile
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		files = append(files, isoSrcFile{rel: rel, src: path, size: info.Size()})
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no casper files to pack")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	const (
		pvdLBA     = 16
		jolietLBA  = 17
		termLBA    = 18
		jRootLBA   = 19
		jCasperLBA = 20
		jDiskLBA   = 21
		dataStart  = 22
	)
	lba := uint32(dataStart)
	for i := range files {
		files[i].lba = lba
		nsec := uint32((files[i].size + isoSector - 1) / isoSector)
		if nsec == 0 {
			nsec = 1
		}
		lba += nsec
	}

	jRoot := make([]byte, isoSector)
	jCasper := make([]byte, isoSector)
	jDisk := make([]byte, isoSector)
	off := 0
	for _, rec := range []struct {
		name string
		lba  uint32
		dir  bool
	}{
		{".", jRootLBA, true},
		{"..", jRootLBA, true},
		{"casper", jCasperLBA, true},
		{".disk", jDiskLBA, true},
	} {
		n := putDirRecJ(jRoot[off:], rec.name, rec.lba, isoSector, rec.dir)
		if n <= 0 {
			return fmt.Errorf("ISO root directory cannot fit %s (install xorriso on the control plane)", rec.name)
		}
		off += n
	}

	cOff, dOff := 0, 0
	n := putDirRecJ(jCasper[cOff:], ".", jCasperLBA, isoSector, true)
	if n <= 0 {
		return fmt.Errorf("ISO casper directory cannot fit")
	}
	cOff += n
	n = putDirRecJ(jCasper[cOff:], "..", jRootLBA, isoSector, true)
	if n <= 0 {
		return fmt.Errorf("ISO casper directory cannot fit")
	}
	cOff += n
	n = putDirRecJ(jDisk[dOff:], ".", jDiskLBA, isoSector, true)
	if n <= 0 {
		return fmt.Errorf("ISO .disk directory cannot fit")
	}
	dOff += n
	n = putDirRecJ(jDisk[dOff:], "..", jRootLBA, isoSector, true)
	if n <= 0 {
		return fmt.Errorf("ISO .disk directory cannot fit")
	}
	dOff += n
	for _, f := range files {
		base := filepath.Base(filepath.FromSlash(f.rel))
		dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(f.rel)))
		switch dir {
		case "casper":
			n = putDirRecJ(jCasper[cOff:], base, f.lba, uint32(f.size), false)
			if n <= 0 {
				return fmt.Errorf("ISO casper directory cannot fit %s (install xorriso on the control plane)", base)
			}
			cOff += n
		case ".disk":
			n = putDirRecJ(jDisk[dOff:], base, f.lba, uint32(f.size), false)
			if n <= 0 {
				return fmt.Errorf("ISO .disk directory cannot fit %s (install xorriso on the control plane)", base)
			}
			dOff += n
		}
	}

	pvd := make([]byte, isoSector)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[40:72], []byte("RAMOS                           "))
	putBoth32ISO(pvd[80:88], lba)
	putBoth16ISO(pvd[128:132], isoSector)
	if putDirRecISO(pvd[156:], []byte{0}, jRootLBA, isoSector, true) <= 0 {
		return fmt.Errorf("ISO PVD root record cannot fit")
	}

	jol := make([]byte, isoSector)
	jol[0] = 2
	copy(jol[1:6], "CD001")
	jol[6] = 1
	jol[88], jol[89], jol[90] = 0x25, 0x2f, 0x45
	putBoth32ISO(jol[80:88], lba)
	putBoth16ISO(jol[128:132], isoSector)
	putJolietPadded(jol[40:72], "RAMOS")
	if putDirRecJ(jol[156:], ".", jRootLBA, isoSector, true) <= 0 {
		return fmt.Errorf("ISO Joliet root record cannot fit")
	}

	term := make([]byte, isoSector)
	term[0] = 255
	copy(term[1:6], "CD001")
	term[6] = 1

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zeros := make([]byte, isoSector)
	for i := 0; i < 16; i++ {
		if _, err := out.Write(zeros); err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	for _, sec := range [][]byte{pvd, jol, term, jRoot, jCasper, jDisk} {
		if _, err := out.Write(sec); err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	buf := make([]byte, isoSector)
	for _, f := range files {
		in, err := os.Open(f.src)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmp)
			return err
		}
		nsec := uint32((f.size + isoSector - 1) / isoSector)
		if nsec == 0 {
			nsec = 1
		}
		need := int64(nsec) * isoSector
		copied := int64(0)
		for copied < need {
			clear(buf)
			if copied < f.size {
				n := isoSector
				left := f.size - copied
				if left < int64(n) {
					n = int(left)
				}
				if _, err := io.ReadFull(in, buf[:n]); err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
					in.Close()
					_ = out.Close()
					_ = os.Remove(tmp)
					return err
				}
			}
			if _, err := out.Write(buf); err != nil {
				in.Close()
				_ = out.Close()
				_ = os.Remove(tmp)
				return err
			}
			copied += isoSector
		}
		in.Close()
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func putBoth32ISO(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b[0:4], v)
	binary.BigEndian.PutUint32(b[4:8], v)
}

func putBoth16ISO(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b[0:2], v)
	binary.BigEndian.PutUint16(b[2:4], v)
}

func putDirRecISO(dst, name []byte, lba, size uint32, dir bool) int {
	nameLen := len(name)
	recLen := 33 + nameLen
	if recLen%2 == 1 {
		recLen++
	}
	if recLen > len(dst) {
		return -1
	}
	dst[0] = byte(recLen)
	putBoth32ISO(dst[2:10], lba)
	putBoth32ISO(dst[10:18], size)
	if dir {
		dst[25] = 0x02
	}
	dst[28] = 1
	dst[30] = 1
	dst[32] = byte(nameLen)
	copy(dst[33:], name)
	return recLen
}

func putJolietPadded(dst []byte, s string) {
	u := utf16.Encode([]rune(s))
	for i := 0; i < len(dst)/2; i++ {
		r := uint16(0x0020)
		if i < len(u) {
			r = u[i]
		}
		binary.BigEndian.PutUint16(dst[i*2:], r)
	}
}

func putDirRecJ(dst []byte, name string, lba, size uint32, dir bool) int {
	var raw []byte
	switch name {
	case ".":
		raw = []byte{0}
	case "..":
		raw = []byte{1}
	default:
		u := utf16.Encode([]rune(name))
		raw = make([]byte, len(u)*2)
		for i, r := range u {
			binary.BigEndian.PutUint16(raw[i*2:], r)
		}
	}
	return putDirRecISO(dst, raw, lba, size, dir)
}
