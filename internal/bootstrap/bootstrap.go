package bootstrap

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Songxwn/Rack-auto/internal/config"
)

func Run(cfg config.Config, agentSrc string, offline bool) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	hc := &http.Client{Timeout: 5 * time.Minute}

	fmt.Println(">> install local iPXE (no boot.ipxe.org)")
	if err := InstallIPXE(cfg.TFTPDir()); err != nil {
		return err
	}

	fmt.Println(">> Ubuntu RAMOS (cache live-server ISO; machines fetch casper.iso only)")
	if err := installUbuntu(hc, cfg, offline); err != nil {
		return err
	}

	fmt.Println(">> cross-compile Linux agent")
	if err := buildAgents(cfg, agentSrc); err != nil {
		fmt.Printf("   ! build agent: %v\n", err)
		if _, err2 := os.Stat(filepath.Join(cfg.AgentDir(), "x86_64", "rackauto-agent")); err2 != nil {
			return err
		}
	}
	fmt.Println("bootstrap done. iPXE and Ubuntu RAMOS are served from local TFTP/HTTP.")
	return nil
}

func download(hc *http.Client, url, dest string) error {
	if st, err := os.Stat(dest); err == nil && st.Size() > 1024 {
		return nil
	}
	resp, err := hc.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", resp.Status)
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func haveLinuxAgent(cfg config.Config) bool {
	for _, arch := range []string{"x86_64", "aarch64"} {
		st, err := os.Stat(filepath.Join(cfg.AgentDir(), arch, "rackauto-agent"))
		if err == nil && st.Size() > 1024 {
			return true
		}
	}
	return false
}

func buildAgents(cfg config.Config, src string) error {
	if src == "" {
		src = "."
	}
	pkg := filepath.Join(src, "cmd", "rackauto-agent")
	if _, err := os.Stat(pkg); err != nil {
		pkg = "./cmd/rackauto-agent"
	}
	if _, err := os.Stat(pkg); err != nil {
		if haveLinuxAgent(cfg) {
			fmt.Println("   no source; keeping existing agent")
			return nil
		}
		return fmt.Errorf("no cmd/rackauto-agent source and no prebuilt agent; copy Release rackauto-agent to %s/<arch>/rackauto-agent", cfg.AgentDir())
	}
	if _, err := exec.LookPath("go"); err != nil {
		if haveLinuxAgent(cfg) {
			fmt.Println("   go not installed; keeping existing agent")
			return nil
		}
		return fmt.Errorf("go not found; use the Release binary on the control plane")
	}
	targets := []struct{ goos, goarch, dir string }{
		{"linux", "amd64", "x86_64"},
		{"linux", "arm64", "aarch64"},
	}
	for _, t := range targets {
		out := filepath.Join(cfg.AgentDir(), t.dir, "rackauto-agent")
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, pkg)
		cmd.Env = append(os.Environ(), "GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if runtime.GOOS != "" {
			fmt.Printf("   go build %s/%s -> %s\n", t.goos, t.goarch, out)
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cross-compile failed (if Go 1.26 reports nfcSparseValues, use GOTOOLCHAIN=go1.25.3): %w", err)
		}
	}
	return nil
}
