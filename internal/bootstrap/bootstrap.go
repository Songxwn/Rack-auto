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

	fmt.Println(">> 安装本机 iPXE（不访问 boot.ipxe.org）")
	if err := InstallIPXE(cfg.TFTPDir()); err != nil {
		return err
	}

	fmt.Println(">> Alpine RAMOS 内核")
	if err := installAlpineNetboot(hc, cfg, offline); err != nil {
		fmt.Printf("   ! %v\n", err)
		if offline {
			return err
		}
	}

	fmt.Println(">> 本地 Alpine APK 仓库（供 RAMOS 离线 apk add）")
	if err := installAlpineRepo(hc, cfg, offline); err != nil {
		fmt.Printf("   ! %v\n", err)
		if offline {
			return err
		}
	}

	fmt.Println(">> 交叉编译 Linux Agent")
	if err := buildAgents(cfg, agentSrc); err != nil {
		fmt.Printf("   ! 编译 agent: %v\n", err)
		if _, err2 := os.Stat(filepath.Join(cfg.AgentDir(), "x86_64", "rackauto-agent")); err2 != nil {
			return err
		}
	}
	fmt.Println("bootstrap 完成。iPXE 与内核均由本机 TFTP/HTTP 提供，装机网不必访问公网。")
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

func buildAgents(cfg config.Config, src string) error {
	if src == "" {
		src = "."
	}
	pkg := filepath.Join(src, "cmd", "rackauto-agent")
	if _, err := os.Stat(pkg); err != nil {
		pkg = "./cmd/rackauto-agent"
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
			return err
		}
	}
	return nil
}
