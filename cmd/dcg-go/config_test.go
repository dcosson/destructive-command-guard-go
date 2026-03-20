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
destructive_policy: strict
privacy_policy: interactive
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
	if cfg.DestructivePolicy != "strict" {
		t.Fatalf("destructive_policy = %q", cfg.DestructivePolicy)
	}
	if cfg.PrivacyPolicy != "interactive" {
		t.Fatalf("privacy_policy = %q", cfg.PrivacyPolicy)
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
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "cannot stat config") {
		t.Fatalf("stderr = %q", stderr.(*bytes.Buffer).String())
	}
}

func TestConfigMissingDefault(t *testing.T) {
	reset := withIO(t)
	defer reset()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("DCG_CONFIG", "")
	cfg := loadConfig()
	if cfg.DestructivePolicy != "" {
		t.Fatalf("destructive_policy = %q, want empty", cfg.DestructivePolicy)
	}
	if cfg.PrivacyPolicy != "" {
		t.Fatalf("privacy_policy = %q, want empty", cfg.PrivacyPolicy)
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
		DestructivePolicy: "strict",
		PrivacyPolicy:     "interactive",
		Allowlist:         []string{"git status *"},
		Blocklist:         []string{"rm -rf /"},
		DisabledPacks:     []string{"platform.github"},
	}
	opts := cfg.toOptions()
	if len(opts) != 5 {
		t.Fatalf("opts len = %d, want 5", len(opts))
	}
}

func TestConfigToOptionsInvalidPolicyWarns(t *testing.T) {
	reset := withIO(t)
	defer reset()

	cfg := Config{
		DestructivePolicy: "invalid",
	}
	opts := cfg.toOptions()
	if len(opts) != 0 {
		t.Fatalf("opts len = %d, want 0", len(opts))
	}
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "warning: destructive_policy:") {
		t.Fatalf("stderr = %q", stderr.(*bytes.Buffer).String())
	}
}

func TestConfigDefaultStatFailureIsFatal(t *testing.T) {
	reset := withIO(t)
	defer reset()

	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir .config: %v", err)
	}
	if err := os.Chmod(cfgDir, 0o000); err != nil {
		t.Fatalf("chmod .config: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(cfgDir, 0o755)
	})

	code := 0
	exitFn = func(c int) { code = c }
	t.Setenv("HOME", home)
	t.Setenv("DCG_CONFIG", "")
	_ = loadConfig()
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.(*bytes.Buffer).String(), "cannot stat config") {
		t.Fatalf("stderr = %q", stderr.(*bytes.Buffer).String())
	}
}
