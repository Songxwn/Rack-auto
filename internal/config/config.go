package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen     string `yaml:"listen"`
	PublicURL  string `yaml:"public_url"`
	DataDir    string `yaml:"data_dir"`
	APIToken   string `yaml:"api_token"`
	TFTPListen string `yaml:"tftp_listen"`
	DHCP       DHCP   `yaml:"dhcp"`
	Bootstrap  Boot   `yaml:"bootstrap"`
}

type DHCP struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Interface  string `yaml:"interface" json:"interface"`
	ListenAddr string `yaml:"listen_addr" json:"listen_addr"`
	Subnet     string `yaml:"subnet" json:"subnet"`
	RangeStart string `yaml:"range_start" json:"range_start"`
	RangeEnd   string `yaml:"range_end" json:"range_end"`
	Router     string `yaml:"router" json:"router"`
	DNS        string `yaml:"dns" json:"dns"`
	LeaseSec   int    `yaml:"lease_sec" json:"lease_sec"`
	NextServer string `yaml:"next_server" json:"next_server"`
	// PXEOnly: when nil, defaults to false — answer all DHCP clients.
	// Set true to ignore non-PXE clients (Option 60/93/97, iPXE, bootfile PRL).
	PXEOnly *bool `yaml:"pxe_only,omitempty" json:"pxe_only,omitempty"`
}

type Boot struct {
	// UbuntuRelease is the live-server release used as RAMOS (latest LTS).
	UbuntuRelease string `yaml:"ubuntu_release"`
	// UbuntuMirror is the directory that contains "<release>/ubuntu-*-live-server-amd64.iso".
	// Empty or "auto" fetches official Ubuntu CD mirror paths (excluding Aliyun)
	// and picks the lowest HTTP latency.
	UbuntuMirror string `yaml:"ubuntu_mirror"`
	// UbuntuCDImage is the ARM (and other ports) image root.
	// Empty or "auto" uses official Ubuntu CD mirror paths (excluding Aliyun).
	// ISO is at <root>/<release>/release/.
	UbuntuCDImage string `yaml:"ubuntu_cdimage"`
	// UbuntuISO is an optional local amd64 live-server ISO (skips download).
	UbuntuISO string `yaml:"ubuntu_iso"`
	// UbuntuISOARM is an optional local arm64 live-server ISO.
	UbuntuISOARM string `yaml:"ubuntu_iso_arm"`
	// UbuntuArches lists RAMOS architectures to cache. Default: x86_64 only.
	UbuntuArches []string `yaml:"ubuntu_arches"`
}

func Default() Config {
	return Config{
		Listen:     ":8080",
		PublicURL:  "http://127.0.0.1:8080",
		DataDir:    "./data",
		TFTPListen: ":69",
		DHCP: DHCP{
			ListenAddr: "0.0.0.0:67",
			LeaseSec:   3600,
			DNS:        "8.8.8.8",
			PXEOnly:    boolPtr(false),
		},
		Bootstrap: Boot{UbuntuRelease: "26.04", UbuntuArches: []string{"x86_64"}},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(expandYAMLIndentTabs(b), &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	cfg.normalize()
	return cfg, nil
}

func (c *Config) normalize() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "./data"
	}
	if c.TFTPListen == "" {
		c.TFTPListen = ":69"
	}
	c.DHCP.Normalize()
	if c.Bootstrap.UbuntuRelease == "" {
		c.Bootstrap.UbuntuRelease = "26.04"
	}
	if len(c.Bootstrap.UbuntuArches) == 0 {
		c.Bootstrap.UbuntuArches = []string{"x86_64"}
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
}

// expandYAMLIndentTabs turns leading tabs into two spaces.
// YAML forbids tab indentation; a pasted example once shipped a tab on ubuntu_cdimage.
func expandYAMLIndentTabs(b []byte) []byte {
	if !bytes.ContainsRune(b, '\t') {
		return b
	}
	lines := bytes.Split(b, []byte("\n"))
	out := make([][]byte, len(lines))
	changed := false
	for i, line := range lines {
		n := 0
		for n < len(line) && line[n] == '\t' {
			n++
		}
		if n == 0 {
			out[i] = line
			continue
		}
		out[i] = append(bytes.Repeat([]byte("  "), n), line[n:]...)
		changed = true
	}
	if !changed {
		return b
	}
	return bytes.Join(out, []byte("\n"))
}

func (c Config) DBPath() string      { return filepath.Join(c.DataDir, "rackauto.db") }
func (c Config) ImagesDir() string   { return filepath.Join(c.DataDir, "images") }
func (c Config) TFTPDir() string     { return filepath.Join(c.DataDir, "tftp") }
func (c Config) RAMOSDir() string    { return filepath.Join(c.DataDir, "ramos") }
func (c Config) AgentDir() string    { return filepath.Join(c.DataDir, "agent") }
func (c Config) OverlayDir() string  { return filepath.Join(c.DataDir, "apkovl") }
func (c Config) WinPEDir() string    { return filepath.Join(c.DataDir, "winpe") }
func (c Config) EnsureDirs() error {
	for _, d := range []string{c.DataDir, c.ImagesDir(), c.TFTPDir(), c.RAMOSDir(), c.AgentDir(), c.OverlayDir(), c.WinPEDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func WriteExample(path string) error {
	cfg := Default()
	cfg.PublicURL = "http://10.0.0.1:8080"
	cfg.APIToken = "change-me"
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
