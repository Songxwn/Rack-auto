package bootstrap

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ubuntuMirror struct {
	Name string
	Root string
	Kind mirrorKind
}

type mirrorKind int

const (
	kindReleases mirrorKind = iota // {root}/{release}/SHA256SUMS
	kindCDImage                    // {root}/{release}/release/SHA256SUMS
)

// Common public mirrors that ship Ubuntu live-server ISOs.
var ubuntuReleaseMirrors = []ubuntuMirror{
	{Name: "Canonical", Root: "https://releases.ubuntu.com", Kind: kindReleases},
	{Name: "阿里云", Root: "https://mirrors.aliyun.com/ubuntu-releases", Kind: kindReleases},
	{Name: "清华 TUNA", Root: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases", Kind: kindReleases},
	{Name: "中科大", Root: "https://mirrors.ustc.edu.cn/ubuntu-releases", Kind: kindReleases},
	{Name: "华为云", Root: "https://mirrors.huaweicloud.com/ubuntu-releases", Kind: kindReleases},
	{Name: "腾讯云", Root: "https://mirrors.cloud.tencent.com/ubuntu-releases", Kind: kindReleases},
	{Name: "网易", Root: "https://mirrors.163.com/ubuntu-releases", Kind: kindReleases},
	{Name: "上海交大", Root: "https://mirror.sjtu.edu.cn/ubuntu-releases", Kind: kindReleases},
	{Name: "北外", Root: "https://mirrors.bfsu.edu.cn/ubuntu-releases", Kind: kindReleases},
	{Name: "南京大学", Root: "https://mirrors.nju.edu.cn/ubuntu-releases", Kind: kindReleases},
}

var ubuntuCDImageMirrors = []ubuntuMirror{
	{Name: "Canonical", Root: "https://cdimage.ubuntu.com/releases", Kind: kindCDImage},
	{Name: "清华 TUNA", Root: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-cdimage/releases", Kind: kindCDImage},
	{Name: "中科大", Root: "https://mirrors.ustc.edu.cn/ubuntu-cdimage/releases", Kind: kindCDImage},
	{Name: "阿里云", Root: "https://mirrors.aliyun.com/ubuntu-cdimage/releases", Kind: kindCDImage},
	{Name: "华为云", Root: "https://mirrors.huaweicloud.com/ubuntu-cdimage/releases", Kind: kindCDImage},
}

type mirrorHit struct {
	name string
	base string
	sums string
	d    time.Duration
	err  error
}

func (m ubuntuMirror) isoDir(rel string) string {
	root := strings.TrimRight(m.Root, "/")
	if m.Kind == kindCDImage {
		return root + "/" + rel + "/release"
	}
	return root + "/" + rel
}

func (m ubuntuMirror) sumsURL(rel string) string {
	return m.isoDir(rel) + "/SHA256SUMS"
}

func isAutoMirror(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "" || s == "auto"
}

func newProbeClient() *http.Client {
	return &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 0,
			}).DialContext,
			TLSHandshakeTimeout:   2 * time.Second,
			ResponseHeaderTimeout: 2 * time.Second,
			IdleConnTimeout:       3 * time.Second,
			DisableKeepAlives:     true,
			ForceAttemptHTTP2:     false,
		},
	}
}

func mirrorsForArch(debArch string) []ubuntuMirror {
	if debArch == "arm64" {
		return ubuntuCDImageMirrors
	}
	return ubuntuReleaseMirrors
}

func pickFastestMirror(hc *http.Client, mirrors []ubuntuMirror, rel string) mirrorHit {
	if hc == nil {
		hc = newProbeClient()
	}
	if len(mirrors) == 0 {
		return mirrorHit{err: fmt.Errorf("没有可用镜像")}
	}
	hits := make([]mirrorHit, len(mirrors))
	var wg sync.WaitGroup
	for i, m := range mirrors {
		wg.Add(1)
		go func(i int, m ubuntuMirror) {
			defer wg.Done()
			hits[i] = probeMirror(hc, m, rel)
		}(i, m)
	}
	wg.Wait()

	var best mirrorHit
	found := false
	for _, h := range hits {
		if h.err != nil {
			fmt.Printf("   - %-12s 失败（%v）\n", h.name, errShort(h.err))
			continue
		}
		fmt.Printf("   - %-12s %s\n", h.name, formatLatency(h.d))
		if !found || h.d < best.d {
			best = h
			found = true
		}
	}
	if !found {
		return mirrorHit{err: fmt.Errorf("所有镜像探测失败")}
	}
	return best
}

func probeMirror(hc *http.Client, m ubuntuMirror, rel string) mirrorHit {
	hit := mirrorHit{name: m.Name, base: m.isoDir(rel)}
	req, err := http.NewRequest(http.MethodGet, m.sumsURL(rel), nil)
	if err != nil {
		hit.err = err
		return hit
	}
	req.Header.Set("User-Agent", "Rack-auto-bootstrap")
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		hit.err = err
		return hit
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		hit.err = fmt.Errorf("%s", resp.Status)
		return hit
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	hit.d = time.Since(start)
	if err != nil {
		hit.err = err
		return hit
	}
	if len(b) < 32 {
		hit.err = fmt.Errorf("SHA256SUMS 为空")
		return hit
	}
	hit.sums = string(b)
	return hit
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return d.Round(time.Millisecond).String()
}

func errShort(err error) string {
	s := err.Error()
	if i := strings.Index(s, "timeout"); i >= 0 {
		return "超时"
	}
	if strings.Contains(s, "no such host") || strings.Contains(s, "server misbehaving") {
		return "DNS 失败"
	}
	if strings.Contains(s, "connection refused") {
		return "连接拒绝"
	}
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
