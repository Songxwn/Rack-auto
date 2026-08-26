package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExampleYAMLParses(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "rackauto.example.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsRune(b, '\t') {
		t.Fatal("example yaml must indent with spaces, not tabs")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bootstrap.UbuntuRelease != "26.04" {
		t.Fatalf("ubuntu_release=%q", cfg.Bootstrap.UbuntuRelease)
	}
}

func TestLoadExpandsIndentTabs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rackauto.yaml")
	src := "listen: \":8080\"\nbootstrap:\n  ubuntu_release: \"26.04\"\n\tubuntu_cdimage: \"https://example.invalid/cdimage\"\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bootstrap.UbuntuCDImage != "https://example.invalid/cdimage" {
		t.Fatalf("ubuntu_cdimage=%q", cfg.Bootstrap.UbuntuCDImage)
	}
}
