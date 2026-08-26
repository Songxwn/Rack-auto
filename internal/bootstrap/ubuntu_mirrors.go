package bootstrap

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	officialCDMirrorsRSS = "https://launchpad.net/ubuntu/+cdmirrors-rss"
	officialReleasesRoot = "https://releases.ubuntu.com"
	officialCDImageRoot  = "https://cdimage.ubuntu.com/releases"
	maxProbeMirrors      = 24
	rssFetchLimit        = 2 << 20
)

type ubuntuMirror struct {
	Name      string
	Root      string
	Kind      mirrorKind
	Country   string
	Bandwidth int
}

type mirrorKind int

const (
	kindReleases mirrorKind = iota // {root}/{release}/SHA256SUMS
	kindCDImage                    // {root}/{release}/release/SHA256SUMS
)

// Used only if Ubuntu's official CD mirror list cannot be fetched.
var fallbackReleaseMirrors = []ubuntuMirror{
	{Name: "Canonical", Root: officialReleasesRoot, Kind: kindReleases},
	{Name: "清华 TUNA", Root: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "中科大", Root: "https://mirrors.ustc.edu.cn/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "华为云", Root: "https://mirrors.huaweicloud.com/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "腾讯云", Root: "https://mirrors.cloud.tencent.com/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "网易", Root: "https://mirrors.163.com/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "上海交大", Root: "https://mirror.sjtu.edu.cn/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "北外", Root: "https://mirrors.bfsu.edu.cn/ubuntu-releases", Kind: kindReleases, Country: "CN"},
	{Name: "南京大学", Root: "https://mirrors.nju.edu.cn/ubuntu-releases", Kind: kindReleases, Country: "CN"},
}

var fallbackCDImageMirrors = []ubuntuMirror{
	{Name: "Canonical", Root: officialCDImageRoot, Kind: kindCDImage},
	{Name: "清华 TUNA", Root: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-cdimage/releases", Kind: kindCDImage, Country: "CN"},
	{Name: "中科大", Root: "https://mirrors.ustc.edu.cn/ubuntu-cdimage/releases", Kind: kindCDImage, Country: "CN"},
	{Name: "华为云", Root: "https://mirrors.huaweicloud.com/ubuntu-cdimage/releases", Kind: kindCDImage, Country: "CN"},
}

type mirrorHit struct {
	name string
	base string
	sums string
	d    time.Duration
	err  error
}

var (
	rssItemRe  = regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	rssLinkRe  = regexp.MustCompile(`(?i)<link>\s*([^<\s]+)\s*</link>`)
	rssTitleRe = regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
	rssBWRe    = regexp.MustCompile(`(?i)<(?:[a-z]+:)?bandwidth>(\d+)</`)
	rssCCRe    = regexp.MustCompile(`(?i)<(?:[a-z]+:)?countrycode>([A-Za-z]{2})</`)
)

func parseOfficialCDMirrors(body []byte) ([]ubuntuMirror, error) {
	chunks := rssItemRe.FindAllSubmatch(body, -1)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("无法解析官方 CD 镜像 RSS")
	}
	var out []ubuntuMirror
	for _, ch := range chunks {
		item := ch[1]
		link := rssField(rssLinkRe, item)
		if skipMirrorURL(link) {
			continue
		}
		name := strings.TrimSpace(rssField(rssTitleRe, item))
		if name == "" {
			name = link
		}
		bw, _ := strconv.Atoi(rssField(rssBWRe, item))
		out = append(out, ubuntuMirror{
			Name:      name,
			Root:      strings.TrimRight(link, "/"),
			Kind:      kindFromRoot(link),
			Country:   strings.ToUpper(rssField(rssCCRe, item)),
			Bandwidth: bw,
		})
	}
	return out, nil
}

func rssField(re *regexp.Regexp, item []byte) string {
	m := re.FindSubmatch(item)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func (m ubuntuMirror) isoDir(rel string) string {
	root := strings.TrimRight(m.Root, "/")
	if m.Kind == kindCDImage {
		low := strings.ToLower(root)
		if strings.Contains(low, "cdimage") && !strings.HasSuffix(low, "/releases") {
			root += "/releases"
		}
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

func skipMirrorURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	if u == "" {
		return true
	}
	if strings.HasPrefix(u, "ftp://") || strings.HasPrefix(u, "rsync://") {
		return true
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return true
	}
	if strings.Contains(u, "aliyun.com") || strings.Contains(u, "aliyuncs.com") {
		return true
	}
	return false
}

func kindFromRoot(root string) mirrorKind {
	u := strings.ToLower(root)
	if strings.Contains(u, "ubuntu-cdimage") || strings.Contains(u, "/cdimage/") {
		return kindCDImage
	}
	return kindReleases
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

func mirrorsForArch(hc *http.Client, debArch string) []ubuntuMirror {
	official, err := fetchOfficialCDMirrors(hc)
	if err != nil {
		fmt.Printf("   ! 官方 CD 镜像列表: %v，改用内置列表（不含阿里云）\n", err)
		if debArch == "arm64" {
			return selectProbeMirrors(fallbackCDImageMirrors, debArch)
		}
		return selectProbeMirrors(fallbackReleaseMirrors, debArch)
	}
	fmt.Printf("   从 Ubuntu 官方取得 %d 条 CD 镜像路径\n", len(official))
	out := selectProbeMirrors(official, debArch)
	if debArch == "arm64" && len(out) < 2 {
		out = selectProbeMirrors(append(append([]ubuntuMirror{}, official...), fallbackCDImageMirrors...), debArch)
	}
	return out
}

func fetchOfficialCDMirrors(hc *http.Client) ([]ubuntuMirror, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, officialCDMirrorsRSS, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Rack-auto-bootstrap")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, rssFetchLimit))
	if err != nil {
		return nil, err
	}
	list, err := parseOfficialCDMirrors(b)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("官方列表为空")
	}
	return list, nil
}

func selectProbeMirrors(all []ubuntuMirror, debArch string) []ubuntuMirror {
	want := kindReleases
	official := ubuntuMirror{Name: "Canonical", Root: officialReleasesRoot, Kind: kindReleases}
	if debArch == "arm64" {
		want = kindCDImage
		official = ubuntuMirror{Name: "Canonical", Root: officialCDImageRoot, Kind: kindCDImage}
	}
	var cn, rest []ubuntuMirror
	seen := map[string]bool{}
	add := func(dst *[]ubuntuMirror, m ubuntuMirror) {
		if skipMirrorURL(m.Root) {
			return
		}
		if m.Kind != want && m.Root != official.Root {
			return
		}
		key := mirrorKey(m.Root)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, m)
	}
	var selected []ubuntuMirror
	add(&selected, official)
	for _, m := range all {
		if strings.EqualFold(m.Country, "CN") {
			cn = append(cn, m)
		} else {
			rest = append(rest, m)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Bandwidth > rest[j].Bandwidth })
	for _, m := range cn {
		if len(selected) >= maxProbeMirrors {
			break
		}
		add(&selected, m)
	}
	for _, m := range rest {
		if len(selected) >= maxProbeMirrors {
			break
		}
		add(&selected, m)
	}
	return selected
}

func mirrorKey(root string) string {
	u, err := url.Parse(strings.TrimSpace(root))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimRight(root, "/"))
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/")
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
	failN := 0
	for _, h := range hits {
		if h.err != nil {
			failN++
			continue
		}
		fmt.Printf("   - %-12s %s\n", h.name, formatLatency(h.d))
		if !found || h.d < best.d {
			best = h
			found = true
		}
	}
	if failN > 0 {
		fmt.Printf("   - %d 个镜像探测失败\n", failN)
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
