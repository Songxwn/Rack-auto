package bootstrap

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Songxwn/Rack-auto/internal/config"
)

var alpineNetboot = []string{"vmlinuz-lts", "initramfs-lts", "modloop-lts"}

// Packages RAMOS needs after apk add. qemu-img lives in community.
var ramosAPKs = []string{
	"parted", "e2fsprogs", "e2fsprogs-extra", "dosfstools", "gptfdisk",
	"util-linux", "dmidecode", "curl", "qemu-img", "grub", "grub-efi",
	"efibootmgr", "nvme-cli", "hdparm", "coreutils", "rsync",
	"openssh-client", "ca-certificates", "musl", "busybox",
}

func alpineCDN(ver, repo, arch, file string) string {
	return fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/v%s/%s/%s/%s", ver, repo, arch, file)
}

func installAlpineNetboot(hc *http.Client, cfg config.Config, offline bool) error {
	ver := cfg.Bootstrap.AlpineVersion
	for _, arch := range []string{"x86_64", "aarch64"} {
		dir := filepath.Join(cfg.RAMOSDir(), arch)
		_ = os.MkdirAll(dir, 0o755)
		base := fmt.Sprintf("https://dl-cdn.alpinelinux.org/alpine/v%s/releases/%s/netboot", ver, arch)
		for _, f := range alpineNetboot {
			dst := filepath.Join(dir, f)
			if st, err := os.Stat(dst); err == nil && st.Size() > 1024 {
				fmt.Println("   +", dst, "(cached)")
				continue
			}
			if offline {
				return fmt.Errorf("离线模式缺少 %s，请先在有网环境执行 bootstrap，或把 Alpine netboot 文件放到 %s", dst, dir)
			}
			if err := download(hc, base+"/"+f, dst); err != nil {
				return fmt.Errorf("%s/%s: %w", arch, f, err)
			}
			fmt.Println("   +", dst)
		}
	}
	return nil
}

func installAlpineRepo(hc *http.Client, cfg config.Config, offline bool) error {
	ver := cfg.Bootstrap.AlpineVersion
	for _, arch := range []string{"x86_64", "aarch64"} {
		merged := map[string]*apkPkg{}
		home := map[string]string{}
		for _, repo := range []string{"main", "community"} {
			dir := filepath.Join(cfg.RAMOSDir(), "alpine", "v"+ver, repo, arch)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			idxPath := filepath.Join(dir, "APKINDEX.tar.gz")
			if _, err := os.Stat(idxPath); err != nil {
				if offline {
					return fmt.Errorf("离线模式缺少 APK 仓库 %s", idxPath)
				}
				if err := download(hc, alpineCDN(ver, repo, arch, "APKINDEX.tar.gz"), idxPath); err != nil {
					return fmt.Errorf("APKINDEX %s/%s: %w", repo, arch, err)
				}
			} else {
				fmt.Println("   +", idxPath, "(cached)")
			}
			pkgs, err := parseAPKIndex(idxPath)
			if err != nil {
				return fmt.Errorf("解析 APKINDEX %s: %w", idxPath, err)
			}
			for n, p := range pkgs {
				if _, ok := merged[n]; !ok {
					merged[n] = p
					home[n] = repo
				}
			}
		}
		need := resolveAPKs(merged, ramosAPKs)
		for _, p := range need {
			repo := home[p.Name]
			if repo == "" {
				continue
			}
			fn := p.Name + "-" + p.Version + ".apk"
			dst := filepath.Join(cfg.RAMOSDir(), "alpine", "v"+ver, repo, arch, fn)
			if st, err := os.Stat(dst); err == nil && st.Size() > 64 {
				continue
			}
			if offline {
				continue
			}
			if err := download(hc, alpineCDN(ver, repo, arch, fn), dst); err != nil {
				fmt.Printf("   ! %s/%s: %v\n", repo, fn, err)
				continue
			}
			fmt.Println("   +", dst)
		}
	}
	return nil
}

type apkPkg struct {
	Name, Version string
	Deps          []string
	Provides      []string
}

func parseAPKIndex(path string) (map[string]*apkPkg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var body []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == "APKINDEX" || strings.HasSuffix(hdr.Name, "APKINDEX") {
			body, err = io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("APKINDEX 为空")
	}
	out := map[string]*apkPkg{}
	var cur *apkPkg
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	flush := func() {
		if cur != nil && cur.Name != "" {
			out[cur.Name] = cur
		}
		cur = nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if len(line) < 2 || line[1] != ':' {
			continue
		}
		if cur == nil {
			cur = &apkPkg{}
		}
		key, val := line[0], line[2:]
		switch key {
		case 'P':
			cur.Name = val
		case 'V':
			cur.Version = val
		case 'D':
			cur.Deps = strings.Fields(val)
		case 'p':
			cur.Provides = strings.Fields(val)
		}
	}
	flush()
	return out, sc.Err()
}

func resolveAPKs(index map[string]*apkPkg, names []string) []*apkPkg {
	provide := map[string]string{}
	for name, p := range index {
		provide[name] = name
		for _, pr := range p.Provides {
			if _, ok := provide[pr]; !ok {
				provide[pr] = name
			}
		}
	}
	seen := map[string]bool{}
	var out []*apkPkg
	var walk func(string)
	walk = func(n string) {
		n = strings.TrimSpace(n)
		if n == "" || strings.HasPrefix(n, "!") {
			return
		}
		if i := strings.IndexAny(n, "<>="); i > 0 {
			n = n[:i]
		}
		pkgName := n
		if strings.Contains(n, ":") {
			if p, ok := provide[n]; ok {
				pkgName = p
			} else {
				return
			}
		} else if _, ok := index[n]; !ok {
			if p, ok := provide[n]; ok {
				pkgName = p
			} else {
				return
			}
		}
		if seen[pkgName] {
			return
		}
		p := index[pkgName]
		if p == nil {
			return
		}
		seen[pkgName] = true
		out = append(out, p)
		for _, d := range p.Deps {
			walk(d)
		}
	}
	for _, n := range names {
		walk(n)
	}
	return out
}
