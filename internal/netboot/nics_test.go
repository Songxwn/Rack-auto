package netboot

import "testing"

func TestListNICs(t *testing.T) {
	nics, err := ListNICs()
	if err != nil {
		t.Fatal(err)
	}
	if nics == nil {
		t.Fatal("nics nil")
	}
}
