package bootstrap

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Songxwn/Rack-auto/internal/config"
)

const defaultUbuntuRelease = "26.04"

func installUbuntu(hc *http.Client, cfg config.Config, offline bool) error {
	rel := cfg.Bootstrap.UbuntuRelease
	if rel == "" {
		rel = defaultUbuntuRelease
	}
	arches := cfg.Bootstrap.UbuntuArches
	if len(arches) == 0 {
		arches = []string{"x86_64"}
	}
	fmt.Printf("   Ubuntu %s live-server；ISO 约 2.7GB，待装机机器建议内存 ≥ 8GB\n", rel)
	isoHC := &http.Client{Timeout: 2 * time.Hour}
	for _, arch := range arches {
		if err := installUbuntuArch(hc, isoHC, cfg, arch, rel, offline); err != nil {
			return err
		}
	}
	return nil
}

func installUbuntuArch(metaHC, isoHC *http.Client, cfg config.Config, arch, rel string, offline bool) error {
	debArch, err := ubuntuDebArch(arch)
	if err != nil {
		return err
	}
	dir := filepath.Join(cfg.RAMOSDir(), "ubuntu", arch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	isoPath := filepath.Join(dir, "live-server.iso")
	vmlinuz := filepath.Join(dir, "vmlinuz")
	initrd := filepath.Join(dir, "initrd")
	if isoReady(isoPath) && kernelReady(vmlinuz, initrd) {
		fmt.Println("   +", isoPath, "(cached)")
		fmt.Println("   +", vmlinuz, "(cached)")
		return nil
	}

	srcISO := localUbuntuISO(cfg, arch)
	if srcISO != "" {
		if err := stageLocalISO(srcISO, isoPath); err != nil {
			return err
		}
	} else if !isoReady(isoPath) {
		if offline {
			return fmt.Errorf("离线模式缺少 %s。请先联网 bootstrap，或把 Ubuntu %s live-server ISO 放到该路径（也可在配置里设 ubuntu_iso）", isoPath, rel)
		}
		name, sum, base, err := resolveLiveServerISO(metaHC, cfg, rel, debArch)
		if err != nil {
			return err
		}
		url := strings.TrimRight(base, "/") + "/" + name
		fmt.Println("   下载", url)
		if err := downloadResume(isoHC, url, isoPath); err != nil {
			return fmt.Errorf("下载 Ubuntu ISO: %w", err)
		}
		if sum != "" {
			if err := verifySHA256(isoPath, sum); err != nil {
				_ = os.Remove(isoPath)
				return err
			}
		}
	} else {
		fmt.Println("   +", isoPath, "(cached)")
	}

	if kernelReady(vmlinuz, initrd) {
		fmt.Println("   +", vmlinuz, "(cached)")
		return nil
	}
	fmt.Println("   从 ISO 抽出 casper/vmlinuz 与 casper/initrd")
	if err := extractCasper(isoPath, vmlinuz, initrd); err != nil {
		return fmt.Errorf("抽出内核: %w", err)
	}
	fmt.Println("   +", vmlinuz)
	fmt.Println("   +", initrd)
	return nil
}

func ubuntuDebArch(arch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("不支持的 RAMOS 架构 %s（请用 x86_64 或 aarch64）", arch)
	}
}

func localUbuntuISO(cfg config.Config, arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64", "arm64":
		return strings.TrimSpace(cfg.Bootstrap.UbuntuISOARM)
	default:
		return strings.TrimSpace(cfg.Bootstrap.UbuntuISO)
	}
}

func isoReady(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 500<<20
}

func kernelReady(vmlinuz, initrd string) bool {
	v, err1 := os.Stat(vmlinuz)
	i, err2 := os.Stat(initrd)
	return err1 == nil && err2 == nil && v.Size() > 2<<20 && i.Size() > 8<<20
}

func stageLocalISO(src, dest string) error {
	st, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("本地 ISO %s: %w", src, err)
	}
	if st.Size() < 500<<20 {
		return fmt.Errorf("本地 ISO 太小，不像 live-server 映像: %s", src)
	}
	if same, err := sameFile(src, dest); err == nil && same {
		return nil
	}
	if isoReady(dest) {
		return nil
	}
	fmt.Println("   使用本地 ISO", src)
	return copyFile(src, dest)
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	return os.Rename(tmp, dest)
}

func ubuntuISOBase(cfg config.Config, rel, debArch string) string {
	if debArch == "arm64" {
		root := strings.TrimSpace(cfg.Bootstrap.UbuntuCDImage)
		if root == "" {
			root = "https://cdimage.ubuntu.com/releases"
		}
		return strings.TrimRight(root, "/") + "/" + rel + "/release"
	}
	mirror := strings.TrimSpace(cfg.Bootstrap.UbuntuMirror)
	if mirror == "" {
		mirror = "https://releases.ubuntu.com"
	}
	return strings.TrimRight(mirror, "/") + "/" + rel
}

func resolveLiveServerISO(hc *http.Client, cfg config.Config, rel, debArch string) (name, sum, base string, err error) {
	base = ubuntuISOBase(cfg, rel, debArch)
	fallback := fmt.Sprintf("ubuntu-%s-live-server-%s.iso", rel, debArch)
	sumsURL := strings.TrimRight(base, "/") + "/SHA256SUMS"
	body, err := httpGetBody(hc, sumsURL)
	if err != nil {
		fmt.Printf("   ! 无法读取 %s（%v），回退文件名 %s\n", sumsURL, err, fallback)
		return fallback, "", base, nil
	}
	name, sum, err = parseLiveServerISO(string(body), debArch)
	if err != nil {
		fmt.Printf("   ! 解析 SHA256SUMS 失败（%v），回退 %s\n", err, fallback)
		return fallback, "", base, nil
	}
	return name, sum, base, nil
}

func parseLiveServerISO(sums, debArch string) (name, sum string, err error) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	suffix := "live-server-" + debArch + ".iso"
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sumPart, filePart, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		filePart = strings.TrimSpace(filePart)
		filePart = strings.TrimPrefix(filePart, "*")
		filePart = strings.TrimSpace(filePart)
		base := filepath.Base(filePart)
		if !strings.HasSuffix(base, suffix) {
			continue
		}
		if strings.Contains(base, ".iso.") {
			continue
		}
		if len(sumPart) != 64 {
			continue
		}
		return base, strings.ToLower(sumPart), nil
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	return "", "", fmt.Errorf("SHA256SUMS 里没有 *%s", suffix)
}

func httpGetBody(hc *http.Client, url string) (string, error) {
	resp, err := hc.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("ISO SHA256 不匹配\n  期望 %s\n  实际 %s", want, got)
	}
	fmt.Println("   SHA256 校验通过")
	return nil
}

func downloadResume(hc *http.Client, url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	var have int64
	if st, err := os.Stat(tmp); err == nil {
		have = st.Size()
		fmt.Printf("   续传 %s 已有 %d MB\n", filepath.Base(dest), have>>20)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var f *os.File
	switch resp.StatusCode {
	case http.StatusPartialContent:
		f, err = os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0o644)
	case http.StatusOK:
		f, err = os.Create(tmp)
		have = 0
	default:
		return fmt.Errorf("%s", resp.Status)
	}
	if err != nil {
		return err
	}
	defer f.Close()

	total := have + resp.ContentLength
	if resp.StatusCode == http.StatusOK && resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	buf := make([]byte, 1<<20)
	var wrote int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			wrote += int64(n)
			if time.Since(last) >= 3*time.Second {
				cur := have + wrote
				if total > 0 {
					fmt.Printf("   ... %d / %d MB\n", cur>>20, total>>20)
				} else {
					fmt.Printf("   ... %d MB\n", cur>>20)
				}
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func extractCasper(iso, vmlinuz, initrd string) error {
	want := map[string]string{
		"casper/vmlinuz": vmlinuz,
		"casper/initrd":  initrd,
	}
	if err := ExtractISOFiles(iso, want); err == nil && fileMin(vmlinuz, 1<<20) && fileMin(initrd, 1<<20) {
		return nil
	} else if err != nil {
		fmt.Printf("   ! Go ISO 解析: %v\n", err)
	}
	if err := extractCasperExternal(iso, filepath.Dir(vmlinuz)); err != nil {
		return fmt.Errorf("无法从 ISO 抽出 casper 内核（可安装 7-Zip/p7zip 后重试）: %w", err)
	}
	if !fileMin(vmlinuz, 1<<20) || !fileMin(initrd, 1<<20) {
		return fmt.Errorf("抽出的 vmlinuz/initrd 不完整")
	}
	return nil
}

func fileMin(path string, n int64) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() >= n
}

func extractCasperExternal(iso, dest string) error {
	type tool struct {
		name string
		args []string
	}
	tools := []tool{
		{"7z", []string{"e", "-y", "-o" + dest, iso, "casper/vmlinuz", "casper/initrd"}},
		{"7za", []string{"e", "-y", "-o" + dest, iso, "casper/vmlinuz", "casper/initrd"}},
		{"7zz", []string{"e", "-y", "-o" + dest, iso, "casper/vmlinuz", "casper/initrd"}},
		{"bsdtar", []string{"-xf", iso, "-C", dest, "casper/vmlinuz", "casper/initrd"}},
	}
	var last error
	for _, t := range tools {
		if _, err := exec.LookPath(t.name); err != nil {
			continue
		}
		cmd := exec.Command(t.name, t.args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			last = err
			continue
		}
		// bsdtar keeps casper/ prefix
		moveIfExists(filepath.Join(dest, "casper", "vmlinuz"), filepath.Join(dest, "vmlinuz"))
		moveIfExists(filepath.Join(dest, "casper", "initrd"), filepath.Join(dest, "initrd"))
		return nil
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("没有 7z/bsdtar，且内置 ISO 解析失败")
}

func moveIfExists(src, dest string) {
	if src == dest {
		return
	}
	if _, err := os.Stat(src); err == nil {
		_ = os.Rename(src, dest)
	}
}
