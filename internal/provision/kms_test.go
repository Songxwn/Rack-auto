package provision_test

import (
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/model"
	"github.com/Songxwn/Rack-auto/internal/provision"
)

func TestListAndLookupKMSKeys(t *testing.T) {
	keys := provision.ListKMSKeys()
	if len(keys) < 9 {
		t.Fatalf("expected 2019-2025 keys, got %d", len(keys))
	}
	k, ok := provision.LookupKMSKey("2022-standard")
	if !ok || k.Key != "VDYBN-27WPP-V4HQT-9VMD4-VMK7H" {
		t.Fatalf("%v %v", ok, k)
	}
	if _, ok := provision.LookupKMSKey("nope"); ok {
		t.Fatal("unknown should miss")
	}
}

func TestMatchKMSKeyID(t *testing.T) {
	got := provision.MatchKMSKeyID("2025", model.WIMImage{
		Name: "Windows Server 2025 SERVERDATACENTER", Flags: "ServerDatacenter",
	})
	if got != "2025-datacenter" {
		t.Fatalf("%s", got)
	}
	got = provision.MatchKMSKeyID("2019", model.WIMImage{
		Name: "Windows Server 2019 SERVERSTANDARDCORE", Flags: "ServerStandardCore",
	})
	if got != "2019-standard" {
		t.Fatalf("core should share standard GVLK: %s", got)
	}
}

func TestUnattendKMSAndDefender(t *testing.T) {
	spec := model.InstallSpec{
		Hostname:       "rack-node-01",
		Username:       "Administrator",
		Password:       "Secret1!",
		Timezone:       "Asia/Shanghai",
		EnableRDP:      true,
		KMSKeyID:       "2022-datacenter",
		KMSHost:        "kms.songxwn.com",
		RemoveDefender: true,
	}
	xml := provision.UnattendXML(spec, "aa:bb:cc:dd:ee:ff")
	for _, want := range []string{
		"slmgr.vbs /ipk WX4NM-KYWYW-QJJR4-XV3QB-6VM33",
		"slmgr.vbs /skms kms.songxwn.com",
		"slmgr.vbs /ato",
		"Test-Connection",
		"Uninstall-WindowsFeature -Name Windows-Defender",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("missing %q in unattend:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "<Key>WX4NM-KYWYW-QJJR4-XV3QB-6VM33</Key>") {
		t.Fatal("GVLK must not go into windowsPE ProductKey (DISM skips that pass)")
	}
	if provision.EffectiveProductKey(model.InstallSpec{ProductKey: "CUSTOM", KMSKeyID: "2022-standard"}) != "CUSTOM" {
		t.Fatal("custom key should win")
	}
}
