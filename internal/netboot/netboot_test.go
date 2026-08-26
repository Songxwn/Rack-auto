package netboot

import (
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
)

func TestNormalizeMAC(t *testing.T) {
	if NormalizeMAC("AA-BB-CC-DD-EE-FF") != "aa:bb:cc:dd:ee:ff" {
		t.Fatal("mac")
	}
}

func TestScriptContainsKernel(t *testing.T) {
	s := &Service{}
	s.Cfg.PublicURL = "http://10.0.0.1:8080"
	out := s.ScriptFor("aa:bb:cc:dd:ee:ff", "x86_64", "efi")
	if !strings.HasPrefix(out, "#!ipxe") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "ramos/ubuntu/x86_64/vmlinuz") || !strings.Contains(out, "casper.iso") {
		t.Fatalf("missing ubuntu ramos boot: %s", out)
	}
	if !strings.Contains(out, "iso-url=") || !strings.Contains(out, "layerfs-path=") {
		t.Fatalf("expected casper netboot: %s", out)
	}
	if !strings.Contains(out, "autoinstall") || !strings.Contains(out, "cloud-config-url=") {
		t.Fatalf("expected autoinstall via cloud-config-url: %s", out)
	}
	if !strings.Contains(out, "/ipxe/cidata/${mac}/user-data") {
		t.Fatalf("expected cidata user-data url: %s", out)
	}
	if strings.Contains(out, "cloud-init=disabled") {
		t.Fatalf("cloud-init=disabled blocks autoinstall: %s", out)
	}
	if !strings.Contains(out, "rackauto_url=") || !strings.Contains(out, "rackauto_mac=") {
		t.Fatalf("expected agent cmdline: %s", out)
	}
	if strings.Contains(out, "live-server.iso") || strings.Contains(out, "nocloud-net") || strings.Contains(out, ";") {
		t.Fatalf("old ISO/autoinstall cmdline: %s", out)
	}
	if strings.Contains(out, "vmlinuz-lts") || strings.Contains(out, "alpine") {
		t.Fatalf("still alpine: %s", out)
	}
}

func TestWindowsPEScript(t *testing.T) {
	s := &Service{}
	out := s.windowsPEScript("http://10.0.0.1:8080", "aa:bb:cc:dd:ee:ff", "x86_64", model.Job{ID: "job1"}, model.Image{ID: "img1"}, model.InstallSpec{})
	if !strings.Contains(out, "/winpe/wimboot") || !strings.Contains(out, "images/win/img1/boot.wim") {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(out, "/ipxe/windows/aa:bb:cc:dd:ee:ff/install.cmd") {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(out, "Windows/System32/winpeshl.ini") {
		t.Fatalf("winpeshl must overlay System32, not WIM root: %s", out)
	}
	if strings.Contains(out, "winpeshl.ini winpeshl.ini") {
		t.Fatalf("winpeshl.ini was left at WIM root: %s", out)
	}
	if !strings.Contains(out, "wimboot index=1") {
		t.Fatalf("expected PE image index: %s", out)
	}
	arm := s.windowsPEScript("http://10.0.0.1:8080", "aa:bb:cc:dd:ee:ff", "arm64", model.Job{ID: "job1"}, model.Image{ID: "img1"}, model.InstallSpec{})
	if !strings.Contains(arm, "x86_64") {
		t.Fatalf("%s", arm)
	}
}

func TestCIData(t *testing.T) {
	s := &Service{}
	s.Cfg.PublicURL = "http://10.0.0.1:8080"
	s.Cfg.APIToken = "secret"
	ud := string(s.CIDataUserData("AA-BB-CC-DD-EE-FF"))
	if !strings.Contains(ud, "autoinstall:") || !strings.Contains(ud, "ramos-start.sh") {
		t.Fatalf("user-data %s", ud)
	}
	if !strings.Contains(ud, "interactive-sections:") {
		t.Fatalf("interactive-sections %s", ud)
	}
	if !strings.Contains(ud, "aa:bb:cc:dd:ee:ff") {
		t.Fatalf("mac in user-data %s", ud)
	}
	md := string(s.CIDataMetaData("aa:bb:cc:dd:ee:ff"))
	if !strings.Contains(md, "instance-id:") {
		t.Fatalf("meta-data %s", md)
	}
	sh := string(s.RamosStart("aa:bb:cc:dd:ee:ff"))
	if !strings.Contains(sh, "rackauto-agent") || !strings.Contains(sh, "sleep infinity") {
		t.Fatalf("start script %s", sh)
	}
	if !strings.Contains(sh, "secret") {
		t.Fatalf("token missing in start script")
	}
	if strings.Contains(sh, "exec >>") {
		t.Fatal("script must keep console output")
	}
	if !strings.Contains(sh, "--max-time") {
		t.Fatal("curl should time out")
	}
	start := strings.Index(sh, "starting rackauto-agent")
	apt := strings.Index(sh, "apt-get update")
	if start < 0 || apt < 0 || apt > start {
		t.Fatal("apt-get must not block agent start")
	}
}
