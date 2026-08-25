package config

import (
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
	Enabled    bool   `yaml:"enabled"`
	Interface  string `yaml:"interface"`
	ListenAddr string `yaml:"listen_addr"`
	Subnet     string `yaml:"subnet"`
	RangeStart string `yaml:"range_start"`
	RangeEnd   string `yaml:"range_end"`
	Router     string `yaml:"router"`
	DNS        string `yaml:"dns"`
	LeaseSec   int    `yaml:"lease_sec"`
}

type Boot struct {
	AlpineVersion string `yaml:"alpine_version"`
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
		},
		Bootstrap: Boot{AlpineVersion: "3.21"},
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
	if err := yaml.Unmarshal(b, &cfg); err != nil {
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
	if c.DHCP.LeaseSec <= 0 {
		c.DHCP.LeaseSec = 3600
	}
	if c.Bootstrap.AlpineVersion == "" {
		c.Bootstrap.AlpineVersion = "3.21"
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
}

func (c Config) DBPath() string      { return filepath.Join(c.DataDir, "rackauto.db") }
func (c Config) ImagesDir() string   { return filepath.Join(c.DataDir, "images") }
func (c Config) TFTPDir() string     { return filepath.Join(c.DataDir, "tftp") }
func (c Config) RAMOSDir() string    { return filepath.Join(c.DataDir, "ramos") }
func (c Config) AgentDir() string    { return filepath.Join(c.DataDir, "agent") }
func (c Config) OverlayDir() string  { return filepath.Join(c.DataDir, "apkovl") }
func (c Config) EnsureDirs() error {
	for _, d := range []string{c.DataDir, c.ImagesDir(), c.TFTPDir(), c.RAMOSDir(), c.AgentDir(), c.OverlayDir()} {
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
