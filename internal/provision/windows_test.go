package provision_test

import (
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func TestWindowsHostnameAndTZ(t *testing.T) {
	if got := provision.WindowsHostname("node-01"); got != "node-01" {
		t.Fatalf("%q", got)
	}
	if got := provision.WindowsHostname("你好服务器"); got != "WIN-HOST" {
		t.Fatalf("non-ascii %q", got)
	}
	if got := provision.WindowsHostname("this-is-a-very-long-hostname"); len(got) > 15 {
		t.Fatalf("len %q", got)
	}
	if provision.WindowsTimeZone("Asia/Shanghai") != "China Standard Time" {
		t.Fatal("tz")
	}
}

func TestWinPEDiskNumber(t *testing.T) {
	if provision.WinPEDiskNumber("/dev/sda") != 0 || provision.WinPEDiskNumber("/dev/sdb") != 1 {
		t.Fatal("sd")
	}
	if provision.WinPEDiskNumber("/dev/nvme1n1") != 1 {
		t.Fatal("nvme")
	}
	if provision.WinPEDiskNumber("disk2") != 2 {
		t.Fatal("disk2")
	}
}

func TestDefaultWIMIndexPrefersGUIStandard(t *testing.T) {
	imgs := []model.WIMImage{
		{Index: 1, Name: "Windows Server 2022 SERVERSTANDARDCORE", Flags: "ServerStandardCore"},
		{Index: 2, Name: "Windows Server 2022 SERVERSTANDARD", Flags: "ServerStandard"},
		{Index: 3, Name: "Windows Server 2022 SERVERDATACENTERCORE", Flags: "ServerDatacenterCore"},
	}
	if got := provision.DefaultWIMIndex(imgs); got != 2 {
		t.Fatalf("index %d", got)
	}
}

func TestUnattendAndDiskpart(t *testing.T) {
	spec := model.InstallSpec{
		Hostname:  "rack-node-01",
		Username:  "Administrator",
		Password:  `p&a<"ss`,
		Timezone:  "Asia/Shanghai",
		Firmware:  model.FirmwareUEFI,
		Disk:      "/dev/sda",
		EnableRDP: true,
		ProductKey: "XXXXX-XXXXX-XXXXX-XXXXX-XXXXX",
		Network: model.NetConfig{NICs: []model.NICConfig{{
			Kind:    model.NICEthernet,
			MAC:     "aa:bb:cc:dd:ee:ff",
			Method:  "static",
			Address: "10.0.0.20/24",
			Gateway: "10.0.0.1",
			DNS:     []string{"8.8.8.8"},
		}}},
	}
	xml := provision.UnattendXML(spec, "aa:bb:cc:dd:ee:ff")
	for _, want := range []string{
		"China Standard Time",
		"rack-node-01",
		"p&amp;a&lt;&#34;ss",
		"XXXXX-XXXXX-XXXXX-XXXXX-XXXXX",
		"AA-BB-CC-DD-EE-FF",
		"10.0.0.20/24",
		"fDenyTSConnections>false",
		"en-US",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("missing %q in unattend:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "zh-CN") {
		t.Fatal("must not force zh-CN")
	}
	dp := provision.DiskpartScript(model.FirmwareUEFI, 0)
	if !strings.Contains(dp, "convert gpt") || !strings.Contains(dp, "assign letter=W") {
		t.Fatalf("diskpart %s", dp)
	}
	bios := provision.DiskpartScript(model.FirmwareBIOS, 1)
	if !strings.Contains(bios, "select disk 1") || !strings.Contains(bios, "convert mbr") {
		t.Fatalf("bios diskpart %s", bios)
	}
	img := model.Image{ID: "img1", Kind: model.ImageWindowsISO, Inspect: &model.ImageInspect{
		Windows: true, WIMImages: []model.WIMImage{{Index: 2, Name: "Standard"}},
	}}
	media := provision.WindowsJobMedia("http://10.0.0.1:8080", "tok%en", "job1", "aa:bb:cc:dd:ee:ff", spec, img)
	if !strings.Contains(media.Install, "dism.exe /Apply-Image") {
		t.Fatal(media.Install)
	}
	if !strings.Contains(media.Install, "tok%%en") {
		t.Fatal("percent in token")
	}
	if !strings.Contains(media.Install, "/Index:2") {
		t.Fatal(media.Install)
	}
	if !strings.Contains(media.Winpeshl, "startnet.cmd") {
		t.Fatal(media.Winpeshl)
	}
	if !strings.Contains(media.Startnet, "WaitForNetwork") || !strings.Contains(media.Startnet, `%~dp0install.cmd`) {
		t.Fatal(media.Startnet)
	}
	if strings.Contains(media.Install, `X:\diskpart.txt`) {
		t.Fatal("install.cmd must use System32 diskpart.txt, not X:\\")
	}
	if !strings.Contains(media.Install, "certutil.exe") || !strings.Contains(media.Install, ":httpget") {
		t.Fatal("WinPE has no curl by default; install.cmd must fall back to certutil")
	}
	if !strings.Contains(media.Install, `curl.exe -fL --retry`) {
		t.Fatal("install.cmd must call injected curl.exe first")
	}
	if !strings.Contains(media.Install, "Invoke-WebRequest") || !strings.Contains(media.Install, "-UseBasicParsing") {
		t.Fatal("WinPE install.cmd must fall back to PowerShell Invoke-WebRequest")
	}
}
