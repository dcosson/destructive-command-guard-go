package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"gopkg.in/yaml.v3"
)

// Config represents the dcg-go YAML configuration file.
type Config struct {
	Policy        string   `yaml:"policy"`
	Allowlist     []string `yaml:"allowlist"`
	Blocklist     []string `yaml:"blocklist"`
	EnabledPacks  []string `yaml:"enabled_packs"`
	DisabledPacks []string `yaml:"disabled_packs"`
}

const maxConfigFileSize = 1 << 20 // 1MB

func configPath() (path string, explicit bool) {
	if p := os.Getenv("DCG_CONFIG"); p != "" {
		return p, true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".config", "dcg-go", "config.yaml"), false
}

func loadConfig() Config {
	path, explicit := configPath()
	if path == "" {
		return Config{}
	}

	fi, err := os.Stat(path)
	if err != nil {
		if explicit || !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "error: cannot stat config at %s: %v\n", path, err)
			exitFn(1)
		}
		return Config{}
	}
	if fi.Size() > maxConfigFileSize {
		fmt.Fprintf(stderr, "error: config at %s too large (%d bytes, max %d)\n", path, fi.Size(), maxConfigFileSize)
		exitFn(1)
		return Config{}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "error: reading config at %s: %v\n", path, err)
		exitFn(1)
		return Config{}
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(stderr, "error: invalid config at %s: %v\n", path, err)
		exitFn(1)
		return Config{}
	}
	return cfg
}

func (c Config) toOptions() []guard.Option {
	var opts []guard.Option

	if c.Policy != "" {
		p, err := parsePolicy(c.Policy)
		if err != nil {
			fmt.Fprintf(stderr, "warning: %v, using default policy\n", err)
		} else {
			opts = append(opts, guard.WithPolicy(p))
		}
	}
	if len(c.Allowlist) > 0 {
		opts = append(opts, guard.WithAllowlist(c.Allowlist...))
	}
	if len(c.Blocklist) > 0 {
		opts = append(opts, guard.WithBlocklist(c.Blocklist...))
	}
	if len(c.EnabledPacks) > 0 {
		opts = append(opts, guard.WithPacks(c.EnabledPacks...))
	}
	if len(c.DisabledPacks) > 0 {
		opts = append(opts, guard.WithDisabledPacks(c.DisabledPacks...))
	}
	return opts
}
