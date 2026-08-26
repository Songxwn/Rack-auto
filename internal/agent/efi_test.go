package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallEFIFallbackFromDebian(t *testing.T) {
	boot, _, grub, _ := efiFallbackNames()
	root := t.TempDir()
	src := filepath.Join(root, "EFI", "debian", grub)
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("grub-bin"), 0644); err != nil {
		t.Fatal(err)
	}
	wrote, err := installEFIFallback(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) == 0 {
		t.Fatal("expected copy")
	}
	got, err := os.ReadFile(filepath.Join(root, "EFI", "BOOT", boot))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "grub-bin" {
		t.Fatalf("%s: %q", boot, got)
	}
}

func TestInstallEFIFallbackShimPreferred(t *testing.T) {
	boot, shim, grub, _ := efiFallbackNames()
	if runtime.GOARCH == "arm64" && shim == "" {
		t.Skip("no shim name")
	}
	root := t.TempDir()
	ubuntu := filepath.Join(root, "EFI", "ubuntu")
	if err := os.MkdirAll(ubuntu, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ubuntu, shim), []byte("shim"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ubuntu, grub), []byte("grub"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := installEFIFallback(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "EFI", "BOOT", boot))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "shim" {
		t.Fatalf("want shim as %s, got %q", boot, got)
	}
	g, err := os.ReadFile(filepath.Join(root, "EFI", "BOOT", grub))
	if err != nil {
		t.Fatal(err)
	}
	if string(g) != "grub" {
		t.Fatalf("grub companion %q", g)
	}
}

func TestInstallEFIFallbackAlreadyPresent(t *testing.T) {
	boot, _, _, _ := efiFallbackNames()
	root := t.TempDir()
	p := filepath.Join(root, "EFI", "BOOT", boot)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	wrote, err := installEFIFallback(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(wrote, " ")
	if !strings.Contains(joined, "already present") {
		t.Fatalf("wrote %v", wrote)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "keep" {
		t.Fatalf("overwrote existing fallback: %q", got)
	}
}

func TestInstallEFIFallbackEmpty(t *testing.T) {
	root := t.TempDir()
	if _, err := installEFIFallback(root); err == nil {
		t.Fatal("empty ESP should fail")
	}
}
