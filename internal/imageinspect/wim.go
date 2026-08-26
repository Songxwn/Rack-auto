package imageinspect

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"

	"github.com/Songxwn/Rack-auto/internal/model"
)

const (
	wimMagic          = "MSWIM\x00\x00\x00"
	wimHeaderSize     = 208
	wimResCompressed  = 0x04
	wimScanChunk      = 1 << 20
)

type wimRes struct {
	Size   int64
	Flags  byte
	Offset int64
	Orig   int64
}

func (r wimRes) end() int64 {
	if r.Offset < 0 || r.Size <= 0 {
		return 0
	}
	return r.Offset + r.Size
}

type wimHdr struct {
	HeaderSize uint32
	Version    uint32
	ImageCount uint32
	BootIndex  uint32
	XML        wimRes
	OffsetTbl  wimRes
	BootMeta   wimRes
	Integrity  wimRes
}

func parseWIMRes(b []byte) wimRes {
	var size uint64
	for i := 0; i < 7; i++ {
		size |= uint64(b[i]) << (8 * i)
	}
	return wimRes{
		Size:   int64(size),
		Flags:  b[7],
		Offset: int64(binary.LittleEndian.Uint64(b[8:16])),
		Orig:   int64(binary.LittleEndian.Uint64(b[16:24])),
	}
}

func readWIMHeader(r io.ReaderAt, base int64) (wimHdr, error) {
	buf := make([]byte, wimHeaderSize)
	n, err := r.ReadAt(buf, base)
	if n < wimHeaderSize {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return wimHdr{}, err
	}
	if string(buf[0:8]) != wimMagic {
		return wimHdr{}, fmt.Errorf("not a WIM")
	}
	h := wimHdr{
		HeaderSize: binary.LittleEndian.Uint32(buf[8:12]),
		Version:    binary.LittleEndian.Uint32(buf[12:16]),
		ImageCount: binary.LittleEndian.Uint32(buf[44:48]),
		OffsetTbl:  parseWIMRes(buf[48:72]),
		XML:        parseWIMRes(buf[72:96]),
		BootMeta:   parseWIMRes(buf[96:120]),
		BootIndex:  binary.LittleEndian.Uint32(buf[120:124]),
		Integrity:  parseWIMRes(buf[124:148]),
	}
	if h.HeaderSize < 208 || h.HeaderSize > 4096 || h.ImageCount == 0 || h.ImageCount > 64 {
		return wimHdr{}, fmt.Errorf("invalid WIM header")
	}
	return h, nil
}

func (h wimHdr) onDiskSize() int64 {
	max := int64(h.HeaderSize)
	for _, r := range []wimRes{h.XML, h.OffsetTbl, h.BootMeta, h.Integrity} {
		if e := r.end(); e > max {
			max = e
		}
	}
	if max < 208 {
		max = 208
	}
	return max
}

// WIMExtent is one WIM/ESD file embedded in a larger blob (ISO or standalone).
type WIMExtent struct {
	Offset int64
	Size   int64
	Count  uint32
	Images []model.WIMImage
}

func (e WIMExtent) IsBootWIM() bool {
	for _, im := range e.Images {
		n := strings.ToLower(im.Name + " " + im.Description + " " + im.Flags + " " + im.Edition)
		if strings.Contains(n, "windows pe") || strings.Contains(n, "winpe") || strings.Contains(n, "setup") {
			return true
		}
	}
	return false
}

func inspectWIMFile(size int64, r io.ReaderAt) *model.ImageInspect {
	out := &model.ImageInspect{
		Status:       "ok",
		Format:       "wim",
		Windows:      true,
		VirtualSizeB: size,
	}
	ext, err := readWIMExtent(r, 0, size)
	if err != nil {
		out.Status = "error"
		out.Message = err.Error()
		return out
	}
	out.WIMImages = ext.Images
	out.InstallSize = ext.Size
	out.InstallOff = 0
	out.Message = wimSummary(ext.Images, false)
	if ver := DetectWindowsVersion(ext.Images); ver != "" {
		out.Message += "; Windows Server " + ver
	}
	return out
}

func readWIMExtent(r io.ReaderAt, offset, limit int64) (WIMExtent, error) {
	h, err := readWIMHeader(r, offset)
	if err != nil {
		return WIMExtent{}, err
	}
	size := h.onDiskSize()
	if limit > 0 && size > limit {
		size = limit
	}
	images := readWIMImages(r, offset, h)
	if len(images) == 0 {
		for i := 1; i <= int(h.ImageCount); i++ {
			images = append(images, model.WIMImage{Index: i, Name: fmt.Sprintf("Image %d", i)})
		}
	}
	return WIMExtent{Offset: offset, Size: size, Count: h.ImageCount, Images: images}, nil
}

func readWIMImages(r io.ReaderAt, base int64, h wimHdr) []model.WIMImage {
	if h.XML.Orig <= 0 || h.XML.Orig > 16<<20 {
		return nil
	}
	if h.XML.Flags&wimResCompressed != 0 {
		return nil
	}
	buf := make([]byte, int(h.XML.Orig))
	if _, err := r.ReadAt(buf, base+h.XML.Offset); err != nil && err != io.EOF {
		return nil
	}
	text := decodeWIMXML(buf)
	if text == "" {
		return nil
	}
	return parseWIMXML(text)
}

func decodeWIMXML(b []byte) string {
	b = bytes.TrimRight(b, "\x00")
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		u := bytesToUTF16LE(b[2:])
		return string(utf16.Decode(u))
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		u := bytesToUTF16BE(b[2:])
		return string(utf16.Decode(u))
	}
	if looksUTF16LE(b) {
		return string(utf16.Decode(bytesToUTF16LE(b)))
	}
	return string(b)
}

func bytesToUTF16LE(b []byte) []uint16 {
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(b[i*2 : i*2+2])
	}
	return out
}

func bytesToUTF16BE(b []byte) []uint16 {
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.BigEndian.Uint16(b[i*2 : i*2+2])
	}
	return out
}

func looksUTF16LE(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	zeros := 0
	for i := 1; i < 64 && i < len(b); i += 2 {
		if b[i] == 0 {
			zeros++
		}
	}
	return zeros >= 8
}

type wimXMLRoot struct {
	XMLName xml.Name     `xml:"WIM"`
	Images  []wimXMLImage `xml:"IMAGE"`
}

type wimXMLImage struct {
	Index       int    `xml:"INDEX,attr"`
	Name        string `xml:"NAME"`
	Description string `xml:"DESCRIPTION"`
	Flags       string `xml:"FLAGS"`
	TotalBytes  int64  `xml:"TOTALBYTES"`
	Windows     struct {
		Arch        string `xml:"ARCH"`
		Edition     string `xml:"EDITIONID"`
		DisplayName string `xml:"DISPLAYNAME"`
		ProductName string `xml:"PRODUCTNAME"`
	} `xml:"WINDOWS"`
}

func parseWIMXML(text string) []model.WIMImage {
	start := strings.Index(strings.ToUpper(text), "<WIM")
	if start >= 0 {
		text = text[start:]
	}
	var root wimXMLRoot
	if err := xml.Unmarshal([]byte(text), &root); err != nil {
		return nil
	}
	var out []model.WIMImage
	for _, im := range root.Images {
		arch := wimArch(im.Windows.Arch)
		name := strings.TrimSpace(im.Name)
		if name == "" {
			name = strings.TrimSpace(im.Windows.DisplayName)
		}
		if name == "" {
			name = strings.TrimSpace(im.Windows.ProductName)
		}
		out = append(out, model.WIMImage{
			Index:       im.Index,
			Name:        name,
			Description: strings.TrimSpace(im.Description),
			Flags:       strings.TrimSpace(im.Flags),
			Edition:     strings.TrimSpace(im.Windows.Edition),
			Arch:        arch,
			SizeB:       im.TotalBytes,
		})
	}
	return out
}

func wimArch(code string) string {
	switch strings.TrimSpace(code) {
	case "0":
		return "x86"
	case "9":
		return "amd64"
	case "12":
		return "arm64"
	default:
		return code
	}
}

func wimSummary(images []model.WIMImage, boot bool) string {
	kind := "install.wim"
	if boot {
		kind = "boot.wim (WinPE)"
	}
	if len(images) == 0 {
		return kind
	}
	names := make([]string, 0, len(images))
	for _, im := range images {
		n := im.Name
		if n == "" {
			n = im.Edition
		}
		if n == "" {
			n = fmt.Sprintf("index %d", im.Index)
		}
		names = append(names, fmt.Sprintf("%d:%s", im.Index, n))
	}
	if len(names) > 6 {
		names = append(names[:6], "...")
	}
	return kind + " editions " + strings.Join(names, ", ")
}

func DetectWindowsVersion(images []model.WIMImage) string {
	blob := ""
	for _, im := range images {
		blob += " " + im.Name + " " + im.Description + " " + im.Flags
	}
	u := strings.ToUpper(blob)
	switch {
	case strings.Contains(u, "2025"):
		return "2025"
	case strings.Contains(u, "2022"):
		return "2022"
	case strings.Contains(u, "2019"):
		return "2019"
	default:
		return ""
	}
}

func FindWIMExtents(r io.ReaderAt, size int64) []WIMExtent {
	if size < wimHeaderSize {
		return nil
	}
	chunk := wimScanChunk
	overlap := 7
	buf := make([]byte, chunk)
	var hits []int64
	for off := int64(0); off < size; {
		n := chunk
		if off+int64(n) > size {
			n = int(size - off)
		}
		got, err := r.ReadAt(buf[:n], off)
		if got == 0 {
			break
		}
		data := buf[:got]
		start := 0
		for {
			i := bytes.Index(data[start:], []byte("MSWIM"))
			if i < 0 {
				break
			}
			i += start
			if i+8 <= len(data) && string(data[i:i+8]) == wimMagic {
				pos := off + int64(i)
				if len(hits) == 0 || hits[len(hits)-1] != pos {
					hits = append(hits, pos)
				}
			}
			start = i + 1
		}
		if err == io.EOF || got < n {
			break
		}
		step := int64(chunk - overlap)
		if step <= 0 {
			step = 1
		}
		off += step
	}
	var out []WIMExtent
	for i, pos := range hits {
		limit := size - pos
		if i+1 < len(hits) {
			limit = hits[i+1] - pos
		}
		ext, err := readWIMExtent(r, pos, limit)
		if err != nil {
			continue
		}
		if ext.Size <= 0 || ext.Size > limit {
			ext.Size = limit
		}
		out = append(out, ext)
	}
	return out
}

func FindWIMExtentsFile(path string) ([]WIMExtent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return FindWIMExtents(f, st.Size()), nil
}
