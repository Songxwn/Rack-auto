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
	if !strings.Contains(out, "vmlinuz-lts") || !strings.Contains(out, "apkovl") {
		t.Fatalf("missing ramos boot: %s", out)
	}
}

func TestAPKOVL(t *testing.T) {
	s := &Service{}
	s.Cfg.PublicURL = "http://10.0.0.1:8080"
	b, err := s.APKOVL("aa:bb:cc:dd:ee:ff")
	if err != nil || len(b) < 100 {
		t.Fatalf("apkovl %v %d", err, len(b))
	}
}
