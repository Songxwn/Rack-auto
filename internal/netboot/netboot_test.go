package netboot

import (
	"strings"
	"testing"
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
	if !strings.Contains(out, "cloud-init=disabled") || !strings.Contains(out, "noprompt") {
		t.Fatalf("expected casper flags: %s", out)
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

func TestCIData(t *testing.T) {
	s := &Service{}
	s.Cfg.PublicURL = "http://10.0.0.1:8080"
	s.Cfg.APIToken = "secret"
	ud := string(s.CIDataUserData("AA-BB-CC-DD-EE-FF"))
	if !strings.Contains(ud, "autoinstall:") || !strings.Contains(ud, "ramos-start.sh") {
		t.Fatalf("user-data %s", ud)
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
}
