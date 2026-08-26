package bootstrap

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMakeInitrdOverlayContainsCasperBottom(t *testing.T) {
	rawgz, err := makeInitrdOverlay(casperBottomScript())
	if err != nil {
		t.Fatal(err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(rawgz))
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("scripts/casper-bottom/99rackauto")) {
		t.Fatal("missing casper-bottom path")
	}
	if !bytes.Contains(body, []byte("ramos-start.sh")) {
		t.Fatal("missing ramos-start fetch")
	}
	if !bytes.Contains(body, []byte("TRAILER!!!")) {
		t.Fatal("missing cpio trailer")
	}
}

func TestAppendInitrdOverlay(t *testing.T) {
	dir := t.TempDir()
	stock := filepath.Join(dir, "stock")
	if err := os.WriteFile(stock, []byte("STOCK"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "initrd")
	if err := appendInitrdOverlay(stock, dest, "#!/bin/sh\n"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte("STOCK")) {
		t.Fatalf("stock prefix missing")
	}
	if len(b) <= 5 {
		t.Fatal("overlay not appended")
	}
}
