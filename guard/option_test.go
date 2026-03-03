package guard

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.policy == nil {
		t.Fatal("default policy is nil")
	}
	if got := cfg.policy.Decide(Assessment{Severity: Medium}); got != Ask {
		t.Fatalf("default policy medium decision = %v, want %v", got, Ask)
	}
	if cfg.allowlist != nil {
		t.Fatalf("default allowlist = %#v, want nil", cfg.allowlist)
	}
	if cfg.blocklist != nil {
		t.Fatalf("default blocklist = %#v, want nil", cfg.blocklist)
	}
	if cfg.enabledPacks != nil {
		t.Fatalf("default enabledPacks = %#v, want nil", cfg.enabledPacks)
	}
	if cfg.disabledPacks != nil {
		t.Fatalf("default disabledPacks = %#v, want nil", cfg.disabledPacks)
	}
	if cfg.callerEnv != nil {
		t.Fatalf("default callerEnv = %#v, want nil", cfg.callerEnv)
	}
}

func TestOptionsApplyInOrder(t *testing.T) {
	cfg := defaultConfig()
	WithPolicy(PermissivePolicy())(&cfg)
	WithPolicy(StrictPolicy())(&cfg)
	if got := cfg.policy.Decide(Assessment{Severity: Medium}); got != Deny {
		t.Fatalf("later WithPolicy should win, got %v want %v", got, Deny)
	}
}

func TestWithAllowlistAppends(t *testing.T) {
	cfg := defaultConfig()
	WithAllowlist("git status", "ls *")(&cfg)
	WithAllowlist("cat *")(&cfg)
	want := []string{"git status", "ls *", "cat *"}
	if len(cfg.allowlist) != len(want) {
		t.Fatalf("allowlist len = %d, want %d", len(cfg.allowlist), len(want))
	}
	for i := range want {
		if cfg.allowlist[i] != want[i] {
			t.Fatalf("allowlist[%d] = %q, want %q", i, cfg.allowlist[i], want[i])
		}
	}
}

func TestWithBlocklistAppends(t *testing.T) {
	cfg := defaultConfig()
	WithBlocklist("rm *", "mkfs *")(&cfg)
	WithBlocklist("dd *")(&cfg)
	want := []string{"rm *", "mkfs *", "dd *"}
	if len(cfg.blocklist) != len(want) {
		t.Fatalf("blocklist len = %d, want %d", len(cfg.blocklist), len(want))
	}
	for i := range want {
		if cfg.blocklist[i] != want[i] {
			t.Fatalf("blocklist[%d] = %q, want %q", i, cfg.blocklist[i], want[i])
		}
	}
}

func TestWithPacksSetsExplicitSlice(t *testing.T) {
	cfg := defaultConfig()
	WithPacks("core.git", "core.filesystem")(&cfg)
	if len(cfg.enabledPacks) != 2 || cfg.enabledPacks[0] != "core.git" || cfg.enabledPacks[1] != "core.filesystem" {
		t.Fatalf("enabledPacks = %#v", cfg.enabledPacks)
	}

	WithPacks()(&cfg)
	if cfg.enabledPacks == nil {
		t.Fatal("enabledPacks is nil after WithPacks(); want empty slice")
	}
	if len(cfg.enabledPacks) != 0 {
		t.Fatalf("enabledPacks len = %d, want 0", len(cfg.enabledPacks))
	}
}

func TestWithDisabledPacksAppends(t *testing.T) {
	cfg := defaultConfig()
	WithDisabledPacks("core.git")(&cfg)
	WithDisabledPacks("cloud.aws", "frameworks")(&cfg)
	want := []string{"core.git", "cloud.aws", "frameworks"}
	if len(cfg.disabledPacks) != len(want) {
		t.Fatalf("disabledPacks len = %d, want %d", len(cfg.disabledPacks), len(want))
	}
	for i := range want {
		if cfg.disabledPacks[i] != want[i] {
			t.Fatalf("disabledPacks[%d] = %q, want %q", i, cfg.disabledPacks[i], want[i])
		}
	}
}

func TestWithEnvSetsCallerEnv(t *testing.T) {
	cfg := defaultConfig()
	env := []string{"FOO=bar", "RAILS_ENV=production"}
	WithEnv(env)(&cfg)
	if len(cfg.callerEnv) != len(env) {
		t.Fatalf("callerEnv len = %d, want %d", len(cfg.callerEnv), len(env))
	}
	for i := range env {
		if cfg.callerEnv[i] != env[i] {
			t.Fatalf("callerEnv[%d] = %q, want %q", i, cfg.callerEnv[i], env[i])
		}
	}
}
