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

func Run(cfg config.Config, agentSrc string) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	hc := &http.Client{Timeout: 5 * time.Minute}
	fmt.Println(">> 下载 iPXE 引导文件")
	ipxe := map[string]string{
		"undionly.kpxe": "https://boot.ipxe.org/undionly.kpxe",
		"ipxe.efi":      "https://boot.ipxe.org/ipxe.efi",
		"snponly.efi":   "https://boot.ipxe.org/snponly.efi",
	}
	for name, url := range ipxe {
		dst := filepath.Join(cfg.TFTPDir(), name)
		if err := download(hc, url, dst); err != nil {
			fmt.Printf("   ! %s: %v（可稍后手动放入 %s）\n", name, err, cfg.TFTPDir())
		} else {
			fmt.Println("   +", dst)
		}
	}

	ver := cfg.Bootstrap.AlpineVersion
	fmt.Println(">> 下载 Alpine RAMOS 内核 (v" + ver + ")")
	for _, arch := range []string{"x86_64", "aarch64"} {
		dir := filepath.Join(cfg.RAMOSDir(), arch)
		_ = os.MkdirAll(dir, 0o755)
		base := fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/v%s/releases/%s/netboot", ver, arch)
		for _, f := range []string{"vmlinuz-lts", "initramfs-lts", "modloop-lts"} {
			if err := download(hc, base+"/"+f, filepath.Join(dir, f)); err != nil {
				fmt.Printf("   ! %s/%s: %v\n", arch, f, err)
			} else {
				fmt.Printf("   + %s/%s\n", arch, f)
			}
		}
	}

	fmt.Println(">> 交叉编译 Linux Agent")
	if err := buildAgents(cfg, agentSrc); err != nil {
		fmt.Printf("   ! 编译 agent: %v\n", err)
	}
	fmt.Println("bootstrap 完成。请将 DHCP next-server 指向本机，filename 使用 undionly.kpxe（BIOS）或 ipxe.efi（UEFI）。")
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
