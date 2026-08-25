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
