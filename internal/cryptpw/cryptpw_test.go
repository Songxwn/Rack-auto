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
}
