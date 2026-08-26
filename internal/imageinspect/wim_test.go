package imageinspect

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Songxwn/Rack-auto/internal/model"
)

const sampleWIMXML = `<?xml version="1.0"?><WIM><IMAGE INDEX="1"><NAME>Windows Server 2022 SERVERSTANDARDCORE</NAME><DESCRIPTION>Standard Core</DESCRIPTION><FLAGS>ServerStandardCore</FLAGS><WINDOWS><ARCH>9</ARCH><EDITIONID>ServerStandard</EDITIONID></WINDOWS></IMAGE><IMAGE INDEX="2"><NAME>Windows Server 2022 SERVERSTANDARD</NAME><DESCRIPTION>Standard</DESCRIPTION><FLAGS>ServerStandard</FLAGS><WINDOWS><ARCH>9</ARCH><EDITIONID>ServerStandard</EDITIONID></WINDOWS></IMAGE></WIM>`

func TestParseWIMXML(t *testing.T) {
	got := parseWIMXML(sampleWIMXML)
	if len(got) != 2 || got[1].Name != "Windows Server 2022 SERVERSTANDARD" {
		t.Fatalf("%+v", got)
	}
}

func TestDecodeWIMXMLUTF16(t *testing.T) {
	payload := utf16LEBOM(sampleWIMXML)
	got := decodeWIMXML(append(payload, 0, 0, 0, 0))
	if !strings.Contains(got, "Windows Server 2022 SERVERSTANDARD") {
		t.Fatalf("decoded %q", got)
	}
}

func TestInspectWIMXML(t *testing.T) {
	raw := makeWIM(t, sampleWIMXML)
	path := filepath.Join(t.TempDir(), "install.wim")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got := File(path)
	if !got.Windows || got.Format != "wim" {
		t.Fatalf("%+v", got)
	}
	if len(got.WIMImages) != 2 {
		t.Fatalf("images %+v", got.WIMImages)
	}
	if got.WIMImages[1].Name != "Windows Server 2022 SERVERSTANDARD" {
		t.Fatalf("name %q message %q", got.WIMImages[1].Name, got.Message)
	}
	if DetectWindowsVersion(got.WIMImages) != "2022" {
		t.Fatal(got.Message)
	}
	if err := got.Compatible(model.ImageWindowsWIM, model.FirmwareUEFI); err != nil {
		t.Fatal(err)
	}
}

func utf16LEBOM(xml string) []byte {
	u := utf16.Encode([]rune(xml))
	payload := []byte{0xFF, 0xFE}
	for _, x := range u {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], x)
		payload = append(payload, b[:]...)
	}
	return payload
}

func makeWIM(t *testing.T, xml string) []byte {
	t.Helper()
	payload := utf16LEBOM(xml)
	hdr := make([]byte, 208)
	copy(hdr[0:8], wimMagic)
	binary.LittleEndian.PutUint32(hdr[8:12], 208)
	binary.LittleEndian.PutUint32(hdr[12:16], 0x00010d00)
	binary.LittleEndian.PutUint16(hdr[40:42], 1)
	binary.LittleEndian.PutUint16(hdr[42:44], 1)
	binary.LittleEndian.PutUint32(hdr[44:48], 2)
	putRes := func(at int, size, offset, orig int64) {
		for i := 0; i < 7; i++ {
			hdr[at+i] = byte(size >> (8 * i))
		}
		binary.LittleEndian.PutUint64(hdr[at+8:at+16], uint64(offset))
		binary.LittleEndian.PutUint64(hdr[at+16:at+24], uint64(orig))
	}
	putRes(72, int64(len(payload)), 208, int64(len(payload)))
	return append(hdr, payload...)
}

func TestCompatibleWindowsSkipsLinuxRoot(t *testing.T) {
	in := &model.ImageInspect{Status: "ok", Windows: true, WIMImages: []model.WIMImage{{Index: 1, Name: "Standard"}}}
	if err := in.Compatible(model.ImageWindowsISO, model.FirmwareUEFI); err != nil {
		t.Fatal(err)
	}
}
