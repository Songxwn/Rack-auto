package server_test

import (
	"net/http"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/config"
	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/netboot"
	"github.com/Songxwn/Rack-auto/internal/server"
	"github.com/Songxwn/Rack-auto/internal/store"
)

func TestDeleteJobClearsStuckInstall(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := server.New(cfg, st, netboot.New(cfg, st)).Handler()

	m := model.Machine{Name: "n1", MAC: "aa:bb:cc:dd:ee:ff", Status: model.MachineInstalling}
	if err := st.UpsertMachine(&m); err != nil {
		t.Fatal(err)
	}
	j := model.Job{Type: model.JobInstall, MachineID: m.ID, Status: model.JobPending, Message: "等待 PXE 进入 Windows PE"}
	if err := st.InsertJob(&j); err != nil {
		t.Fatal(err)
	}

	rec := doReq(h, "DELETE", "/api/v1/jobs/"+j.ID, "", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("delete without login: %d %s", rec.Code, rec.Body.String())
	}

	rec = doReq(h, "POST", "/api/v1/login", `{"username":"admin","password":"admin"}`, "", nil)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	ck := cookieFrom(rec)

	rec = doReq(h, "DELETE", "/api/v1/jobs/"+j.ID, "", ck, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete job: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetJob(j.ID); err == nil {
		t.Fatal("job still present after delete")
	}
	got, err := st.GetMachine(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.MachineReady {
		t.Fatalf("machine status %q, want ready", got.Status)
	}

	rec = doReq(h, "DELETE", "/api/v1/jobs/"+j.ID, "", ck, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d %s", rec.Code, rec.Body.String())
	}
}
