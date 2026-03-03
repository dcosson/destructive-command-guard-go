package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigLoading(t *testing.T) {
	reset := withIO(t)
	defer reset()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(`
policy: strict
allowlist:
  - "git status *"
blocklist:
  - "rm -rf /"
disabled_packs:
  - platform.github
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DCG_CONFIG", configFile)
	cfg := loadConfig()
	if cfg.Policy != "strict" {
		t.Fatalf("policy = %q", cfg.Policy)
	}
	if len(cfg.Allowlist) != 1 || cfg.Allowlist[0] != "git status *" {
		t.Fatalf("allowlist = %#v", cfg.Allowlist)
	}
	if len(cfg.Blocklist) != 1 || cfg.Blocklist[0] != "rm -rf /" {
		t.Fatalf("blocklist = %#v", cfg.Blocklist)
	}
	if len(cfg.DisabledPacks) != 1 || cfg.DisabledPacks[0] != "platform.github" {
		t.Fatalf("disabled_packs = %#v", cfg.DisabledPacks)
	}
}

func TestConfigMissingExplicit(t *testing.T) {
	reset := withIO(t)
	defer reset()

	code := 0
	exitFn = func(c int) { code = c }
	t.Setenv("DCG_CONFIG", "/nonexistent/config.yaml")
	_ = loadConfig()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "config not found") {
		t.Fatalf("stderr = %q", stderr.(*bytes.Buffer).String())
	}
}

func TestConfigMissingDefault(t *testing.T) {
	reset := withIO(t)
	defer reset()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("DCG_CONFIG", "")
	cfg := loadConfig()
	if cfg.Policy != "" {
		t.Fatalf("policy = %q, want empty", cfg.Policy)
	}
	if cfg.Allowlist != nil {
		t.Fatalf("allowlist = %#v, want nil", cfg.Allowlist)
	}
}

func TestConfigMalformed(t *testing.T) {
	reset := withIO(t)
	defer reset()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("not: [valid: yaml: {{"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	code := 0
	exitFn = func(c int) { code = c }
	t.Setenv("DCG_CONFIG", configFile)
	_ = loadConfig()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestConfigTooLarge(t *testing.T) {
	reset := withIO(t)
	defer reset()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	data := make([]byte, maxConfigFileSize+1)
	for i := range data {
		data[i] = 'a'
	}
	if err := os.WriteFile(configFile, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	code := 0
	exitFn = func(c int) { code = c }
	t.Setenv("DCG_CONFIG", configFile)
	_ = loadConfig()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestConfigToOptions(t *testing.T) {
	reset := withIO(t)
	defer reset()

	cfg := Config{
		Policy:        "strict",
		Allowlist:     []string{"git status *"},
		Blocklist:     []string{"rm -rf /"},
		DisabledPacks: []string{"platform.github"},
	}
	opts := cfg.toOptions()
	if len(opts) != 4 {
		t.Fatalf("opts len = %d, want 4", len(opts))
	}
}

func TestConfigToOptionsInvalidPolicyWarns(t *testing.T) {
	reset := withIO(t)
	defer reset()

	cfg := Config{
		Policy: "invalid",
	}
	opts := cfg.toOptions()
	if len(opts) != 0 {
		t.Fatalf("opts len = %d, want 0", len(opts))
	}
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "warning: unknown policy") {
		t.Fatalf("stderr = %q", stderr.(*bytes.Buffer).String())
	}
}
