package bootstrap

import "testing"

func TestResolveAPKsFollowsDeps(t *testing.T) {
	idx := map[string]*apkPkg{
		"parted": {Name: "parted", Version: "1-r0", Deps: []string{"so:libc.musl-x86_64.so.1"}},
		"musl":   {Name: "musl", Version: "1-r0", Provides: []string{"so:libc.musl-x86_64.so.1"}},
	}
	got := resolveAPKs(idx, []string{"parted"})
	if len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
}
