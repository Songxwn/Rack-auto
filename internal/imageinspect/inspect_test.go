package imageinspect

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func TestInspectGPTBootable(t *testing.T) {
	raw := makeGPTImage(t)
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got := File(path)
	if got.Status == "error" {
		t.Fatal(got.Message)
	}
	if got.Format != "raw" || got.Table != "gpt" {
		t.Fatalf("format/table %+v", got)
	}
	if !got.BootUEFI || !got.BootBIOS {
		t.Fatalf("boot flags %+v", got)
	}
	if got.RootFS != "ext4" || got.RootNum != 1 {
		t.Fatalf("root %+v", got)
	}
	if got.ESPNum != 15 {
		t.Fatalf("esp num %+v", got)
	}
	if got.EFILoader == "" {
		t.Fatalf("efi loader %+v", got)
	}
	if err := got.Compatible(model.ImageCloudDisk, model.FirmwareUEFI); err != nil {
		t.Fatal(err)
	}
	if err := got.Compatible(model.ImageCloudDisk, model.FirmwareBIOS); err != nil {
		t.Fatal(err)
	}
	if err := got.Compatible(model.ImageCloudRoot, model.FirmwareUEFI); err == nil {
		t.Fatal("whole-disk image must not be used as cloud-root")
	}
}

func TestInspectRootFSOnly(t *testing.T) {
	raw := make([]byte, 4096)
	raw[1024+0x38] = 0x53
	raw[1024+0x39] = 0xEF
	path := filepath.Join(t.TempDir(), "root.img")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got := File(path)
	if got.RootFS != "ext4" || got.Table != "none" {
		t.Fatalf("%+v", got)
	}
	if err := got.Compatible(model.ImageCloudRoot, model.FirmwareUEFI); err != nil {
		t.Fatal(err)
	}
	if err := got.Compatible(model.ImageCloudDisk, model.FirmwareUEFI); err == nil {
		t.Fatal("whole-disk without ESP should fail UEFI")
	}
}

func TestCompatibleNilSkipped(t *testing.T) {
	var in *model.ImageInspect
	if err := in.Compatible(model.ImageCloudDisk, model.FirmwareUEFI); err != nil {
		t.Fatal(err)
	}
	in = &model.ImageInspect{Status: "skipped"}
	if err := in.Compatible(model.ImageCloudDisk, model.FirmwareUEFI); err != nil {
		t.Fatal(err)
	}
}

func makeGPTImage(t *testing.T) []byte {
	t.Helper()
	const sec = 512
	const nsec = 8192
	raw := make([]byte, nsec*sec)
	raw[510], raw[511] = 0x55, 0xAA
	raw[446+4] = 0xEE

	hdr := raw[sec : 2*sec]
	copy(hdr[0:8], "EFI PART")
	binary.LittleEndian.PutUint32(hdr[8:12], 0x00010000)
	binary.LittleEndian.PutUint32(hdr[12:16], 92)
	binary.LittleEndian.PutUint64(hdr[24:32], 1)
	binary.LittleEndian.PutUint64(hdr[40:48], 34)
	binary.LittleEndian.PutUint64(hdr[48:56], nsec-34)
	binary.LittleEndian.PutUint64(hdr[72:80], 2)
	binary.LittleEndian.PutUint32(hdr[80:84], 128)
	binary.LittleEndian.PutUint32(hdr[84:88], 128)

	ents := raw[2*sec:]
	putGPT := func(idx int, typ [16]byte, first, last uint64) {
		e := ents[idx*128 : idx*128+128]
		copy(e[0:16], typ[:])
		e[16] = 1
		binary.LittleEndian.PutUint64(e[32:40], first)
		binary.LittleEndian.PutUint64(e[40:48], last)
	}
	putGPT(0, guidLinux, 2048, 5000)
	putGPT(13, guidBIOS, 40, 2047)
	putGPT(14, guidESP, 5001, 6000)

	off := 2048 * sec
	raw[off+1024+0x38] = 0x53
	raw[off+1024+0x39] = 0xEF

	esp := 5001 * sec
	raw[esp] = 0xEB
	raw[esp+510], raw[esp+511] = 0x55, 0xAA
	copy(raw[esp+82:esp+90], "FAT32   ")
	copy(raw[esp+1024:], []byte("BOOTX64 EFI"))
	return raw
}

func TestGUIDRoundTrip(t *testing.T) {
	if bytes.Equal(guidESP[:], make([]byte, 16)) {
		t.Fatal("zero guid")
	}
}
