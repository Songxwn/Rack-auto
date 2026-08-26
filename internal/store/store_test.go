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
	if err := st.SetMachineStatus(m.ID, model.MachineInstalling); err != nil {
		t.Fatal(err)
	}
	active, err := st.HasActiveJob(m.ID, "")
	if err != nil || !active {
		t.Fatalf("expected active job: %v %v", err, active)
	}
	if err := st.DeleteJob(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetJob(j.ID); err == nil {
		t.Fatal("deleted job still present")
	}
	active, err = st.HasActiveJob(m.ID, "")
	if err != nil || active {
		t.Fatalf("expected no active job: %v %v", err, active)
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

func TestCredentialTemplateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	acct := model.CredentialTemplate{
		Kind:     model.TemplateAccount,
		Name:     "机房 root",
		Username: "root",
		Password: "secret",
		SSHKeys:  []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIacct"},
		Notes:    "ops",
	}
	if err := st.UpsertCredTemplate(&acct); err != nil {
		t.Fatal(err)
	}
	if acct.ID == "" {
		t.Fatal("missing id")
	}
	got, err := st.GetCredTemplate(acct.ID)
	if err != nil || got.Name != "机房 root" || got.Username != "root" || got.Password != "secret" || len(got.SSHKeys) != 1 || got.Notes != "ops" {
		t.Fatalf("get account: %v %#v", err, got)
	}
	key := model.CredentialTemplate{
		Kind:    model.TemplateKey,
		Name:    "运维公钥",
		SSHKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIkey1", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQkey2"},
	}
	if err := st.UpsertCredTemplate(&key); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListCredTemplates()
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %#v", err, list)
	}
	got.Name = "机房 root v2"
	got.Password = "newpass"
	if err := st.UpsertCredTemplate(&got); err != nil {
		t.Fatal(err)
	}
	again, err := st.GetCredTemplate(acct.ID)
	if err != nil || again.Name != "机房 root v2" || again.Password != "newpass" || again.Username != "root" {
		t.Fatalf("update: %v %#v", err, again)
	}
	if err := st.DeleteCredTemplate(key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetCredTemplate(key.ID); err == nil {
		t.Fatal("expected missing key template")
	}
	left, err := st.ListCredTemplates()
	if err != nil || len(left) != 1 || left[0].ID != acct.ID {
		t.Fatalf("after delete: %v %#v", err, left)
	}
}
