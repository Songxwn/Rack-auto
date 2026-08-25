package netboot

import (
	"net"
	"strings"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

func TestIPXEChainURL(t *testing.T) {
	ip := net.ParseIP("192.168.177.1")
	got := IPXEChainURL(ip, "http://10.0.0.1:8080", ":8080")
	want := "http://192.168.177.1:8080/ipxe/boot.ipxe"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestBootFilenameIPXEUsesHTTP(t *testing.T) {
	mac, _ := net.ParseMAC("bc:24:11:66:6b:a9")
	m, err := dhcpv4.NewDiscovery(mac, dhcpv4.WithUserClass("iPXE", false))
	if err != nil {
		t.Fatal(err)
	}
	if got := BootFilename(m); got != "boot.ipxe" {
		t.Fatalf("got %s", got)
	}
}

func TestBootFilenameBIOSUsesUndionly(t *testing.T) {
	mac, _ := net.ParseMAC("bc:24:11:66:6b:a9")
	m, err := dhcpv4.NewDiscovery(mac)
	if err != nil {
		t.Fatal(err)
	}
	if got := BootFilename(m); got != "undionly.kpxe" {
		t.Fatalf("got %s", got)
	}
}

func TestIsIPXEClient(t *testing.T) {
	mac, _ := net.ParseMAC("bc:24:11:66:6b:a9")
	plain, _ := dhcpv4.NewDiscovery(mac)
	if IsIPXEClient(plain) {
		t.Fatal("plain PXE should not look like iPXE")
	}
	ipxe, _ := dhcpv4.NewDiscovery(mac, dhcpv4.WithUserClass("iPXE", false))
	if !IsIPXEClient(ipxe) {
		t.Fatal("user-class iPXE")
	}
}

func TestMenuScriptLocalIsOffline(t *testing.T) {
	s := &Service{}
	s.Cfg.Listen = ":8080"
	s.Cfg.PublicURL = "http://10.0.0.1:8080"
	out := s.MenuScriptLocal()
	if !strings.Contains(out, "http://${next-server}:8080") {
		t.Fatal(out)
	}
	if strings.Contains(out, "boot.ipxe.org") || strings.Contains(out, "ipxe.org/") {
		t.Fatal("must not chain to the internet")
	}
}

func TestMenuScriptBaseKeepsRequestHost(t *testing.T) {
	s := &Service{}
	s.Cfg.PublicURL = "http://10.0.0.1:8080"
	out := s.MenuScriptBase("http://192.168.177.1:8080")
	if !strings.Contains(out, "set base http://192.168.177.1:8080") {
		t.Fatal(out)
	}
	if strings.Contains(out, "10.0.0.1") {
		t.Fatal("should not keep wrong public_url host")
	}
}
