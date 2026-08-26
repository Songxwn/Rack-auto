package cryptpw_test

import (
	"strings"
	"testing"

	"github.com/Songxwn/Rack-auto/internal/cryptpw"
)

func TestSHA512(t *testing.T) {
	h, err := cryptpw.SHA512("rackauto")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$6$") {
		t.Fatalf("got %s", h)
	}
	if !cryptpw.Compare(h, "rackauto") {
		t.Fatal("compare should accept the original password")
	}
	if cryptpw.Compare(h, "wrong") || cryptpw.Compare("", "rackauto") || cryptpw.Compare(h, "") {
		t.Fatal("compare should reject mismatches")
	}
}
