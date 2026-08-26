package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsAutoMirror(t *testing.T) {
	if !isAutoMirror("") || !isAutoMirror(" auto ") || !isAutoMirror("AUTO") {
		t.Fatal("auto")
	}
	if isAutoMirror("https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases") {
		t.Fatal("pinned")
	}
}

func TestPickFastestMirror(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			http.NotFound(w, r)
			return
		}
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 *ubuntu-26.04-live-server-amd64.iso\n"))
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 *ubuntu-26.04-live-server-amd64.iso\n"))
	}))
	defer fast.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 404)
	}))
	defer dead.Close()

	mirrors := []ubuntuMirror{
		{Name: "slow", Root: slow.URL, Kind: kindReleases},
		{Name: "fast", Root: fast.URL, Kind: kindReleases},
		{Name: "dead", Root: dead.URL, Kind: kindReleases},
	}
	hit := pickFastestMirror(http.DefaultClient, mirrors, "26.04")
	if hit.err != nil {
		t.Fatal(hit.err)
	}
	if hit.name != "fast" {
		t.Fatalf("got %s", hit.name)
	}
	if !strings.Contains(hit.sums, "live-server-amd64.iso") {
		t.Fatalf("sums %s", hit.sums)
	}
}

func TestUbuntuMirrorISODir(t *testing.T) {
	r := ubuntuMirror{Root: "https://mirrors.example/ubuntu-releases", Kind: kindReleases}
	if r.isoDir("26.04") != "https://mirrors.example/ubuntu-releases/26.04" {
		t.Fatal(r.isoDir("26.04"))
	}
	c := ubuntuMirror{Root: "https://mirrors.example/ubuntu-cdimage/releases", Kind: kindCDImage}
	if c.isoDir("26.04") != "https://mirrors.example/ubuntu-cdimage/releases/26.04/release" {
		t.Fatal(c.isoDir("26.04"))
	}
	c2 := ubuntuMirror{Root: "https://mirrors.example/ubuntu-cdimage", Kind: kindCDImage}
	if c2.isoDir("26.04") != "https://mirrors.example/ubuntu-cdimage/releases/26.04/release" {
		t.Fatal(c2.isoDir("26.04"))
	}
}

func TestSkipMirrorURL(t *testing.T) {
	if !skipMirrorURL("https://mirrors.aliyun.com/ubuntu-releases") {
		t.Fatal("aliyun")
	}
	if !skipMirrorURL("ftp://ftp.example.com/ubuntu-releases") {
		t.Fatal("ftp")
	}
	if skipMirrorURL("https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases") {
		t.Fatal("tuna")
	}
}

func TestParseOfficialCDMirrors(t *testing.T) {
	const rss = `<?xml version="1.0"?>
<rss xmlns:mirror="https://launchpad.net/" version="2.0">
<channel>
<item>
<title>Tsinghua University</title>
<link>https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases/</link>
<mirror:bandwidth>110</mirror:bandwidth>
<mirror:location>
<mirror:countrycode>CN</mirror:countrycode>
</mirror:location>
</item>
<item>
<title>Aliyun</title>
<link>https://mirrors.aliyun.com/ubuntu-releases/</link>
<mirror:bandwidth>200</mirror:bandwidth>
<mirror:location>
<mirror:countrycode>CN</mirror:countrycode>
</mirror:location>
</item>
<item>
<title>Example FTP</title>
<link>ftp://ftp.example.com/ubuntu-releases/</link>
</item>
<item>
<title>USTC cdimage</title>
<link>https://mirrors.ustc.edu.cn/ubuntu-cdimage/releases/</link>
<mirror:bandwidth>80</mirror:bandwidth>
<mirror:location>
<mirror:countrycode>CN</mirror:countrycode>
</mirror:location>
</item>
</channel>
</rss>`
	list, err := parseOfficialCDMirrors([]byte(rss))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len %d %+v", len(list), list)
	}
	if list[0].Name != "Tsinghua University" || list[0].Country != "CN" || list[0].Bandwidth != 110 {
		t.Fatalf("%+v", list[0])
	}
	if list[0].Kind != kindReleases {
		t.Fatal("releases kind")
	}
	if list[1].Kind != kindCDImage {
		t.Fatal("cdimage kind")
	}
}

func TestSelectProbeMirrorsSkipsAliyunAndKeepsOfficial(t *testing.T) {
	all := []ubuntuMirror{
		{Name: "Aliyun", Root: "https://mirrors.aliyun.com/ubuntu-releases", Kind: kindReleases, Country: "CN", Bandwidth: 999},
		{Name: "Tsinghua", Root: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu-releases", Kind: kindReleases, Country: "CN", Bandwidth: 110},
		{Name: "Far", Root: "https://mirror.example.com/ubuntu-releases", Kind: kindReleases, Country: "US", Bandwidth: 50},
	}
	got := selectProbeMirrors(all, "amd64")
	var names []string
	for _, m := range got {
		names = append(names, m.Root)
		if strings.Contains(m.Root, "aliyun") {
			t.Fatalf("aliyun leaked: %+v", m)
		}
	}
	if len(got) < 2 || got[0].Root != officialReleasesRoot {
		t.Fatalf("%v", names)
	}
	foundTuna := false
	for _, m := range got {
		if strings.Contains(m.Root, "tuna.tsinghua") {
			foundTuna = true
		}
	}
	if !foundTuna {
		t.Fatalf("missing CN mirror: %v", names)
	}
}

func TestFallbackMirrorsHaveNoAliyun(t *testing.T) {
	for _, m := range append(append([]ubuntuMirror{}, fallbackReleaseMirrors...), fallbackCDImageMirrors...) {
		if strings.Contains(strings.ToLower(m.Root), "aliyun") || skipMirrorURL(m.Root) {
			t.Fatal(m.Root)
		}
	}
}
