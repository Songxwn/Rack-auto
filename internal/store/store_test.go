package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/store"
)

func TestMachineAndJob(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := model.Machine{Name: "n1", MAC: "aa:bb:cc:dd:ee:ff", Status: model.MachineReady, BMCType: model.BMCIPMI}
	if err := st.UpsertMachine(&m); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMachineByMAC("AA:BB:CC:DD:EE:FF")
	if err != nil || got.Name != "n1" {
		t.Fatalf("get mac: %v %#v", err, got)
	}
	j := model.Job{Type: model.JobInstall, MachineID: m.ID, Status: model.JobPending, Params: map[string]any{"password": "secret"}}
	if err := st.InsertJob(&j); err != nil {
		t.Fatal(err)
	}
	next, err := st.NextPendingJob(m.ID)
	if err != nil || next.ID != j.ID {
		t.Fatal(err, next)
	}
	red := store.RedactJob(next)
	if p, ok := red.Params.(map[string]any); !ok || p["password"] != "" {
		t.Fatalf("password not redacted: %#v", red.Params)
	}
}

func TestImageInspectRoundtrip(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	img := model.Image{
		Name: "cloud", URL: "http://x/images/a.qcow2", Kind: model.ImageCloudDisk,
		OSFamily: "debian", OSVersion: "12",
		Inspect: &model.ImageInspect{Status: "ok", BootUEFI: true, BootBIOS: true, RootFS: "ext4", RootNum: 1},
	}
	if err := st.UpsertImage(&img); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetImage(img.ID)
	if err != nil || got.Inspect == nil || !got.Inspect.BootUEFI || got.Inspect.RootFS != "ext4" || got.OSVersion != "12" {
		t.Fatalf("get: %v %#v os=%s/%s", err, got.Inspect, got.OSFamily, got.OSVersion)
	}
	list, err := st.ListImages()
	if err != nil || len(list) != 1 || list[0].Inspect == nil || !list[0].Inspect.BootBIOS {
		t.Fatalf("list: %v %#v", err, list)
	}
}
