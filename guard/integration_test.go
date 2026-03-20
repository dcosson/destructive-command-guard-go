package guard_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestIntegrationGitPushForce(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	result := guard.Evaluate("git push --force origin main")
	if result.Decision == guard.Allow {
		t.Fatalf("decision = %v, want non-Allow", result.Decision)
	}
	if result.Matches[0].Pack != "core.git" {
		t.Fatalf("pack = %q, want core.git", result.Matches[0].Pack)
	}
	if !strings.Contains(result.Matches[0].Rule, "push") {
		t.Fatalf("rule = %q, want contains push", result.Matches[0].Rule)
	}
}

func TestIntegrationRmRf(t *testing.T) {
	skipIfPackMissing(t, "core.filesystem")
	result := guard.Evaluate("rm -rf /")
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
	if result.DestructiveAssessment == nil || result.DestructiveAssessment.Severity != guard.Critical {
		t.Fatalf("assessment = %#v, want critical", result.DestructiveAssessment)
	}
}

func TestIntegrationCompoundCommand(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	result := guard.Evaluate("echo 'deploying' && git push --force origin main")
	if result.Decision == guard.Allow {
		t.Fatalf("decision = %v, want non-Allow", result.Decision)
	}
}

func TestIntegrationEnvEscalation(t *testing.T) {
	skipIfPackMissing(t, "frameworks")
	result := guard.Evaluate("RAILS_ENV=production rails db:reset")
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
	if result.DestructiveAssessment == nil || result.DestructiveAssessment.Severity != guard.Critical {
		t.Fatalf("assessment = %#v, want critical", result.DestructiveAssessment)
	}
	if len(result.Matches) > 0 && !result.Matches[0].EnvEscalated {
		t.Fatalf("expected env escalation on first match")
	}
}

func TestIntegrationAllowlistBypass(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithAllowlist("git push *"))
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("matches len = %d, want 0", len(result.Matches))
	}
}

func TestIntegrationBlocklistDeny(t *testing.T) {
	result := guard.Evaluate("ls -la", guard.WithBlocklist("ls *"))
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches len = %d, want 1", len(result.Matches))
	}
	if result.Matches[0].Pack != "_blocklist" {
		t.Fatalf("pack = %q, want _blocklist", result.Matches[0].Pack)
	}
}

func TestIntegrationSafeCommand(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	result := guard.Evaluate("git status")
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestIntegrationPackSelection(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	result := guard.Evaluate("git push --force", guard.WithDisabledPacks("core.git"))
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestIntegrationWithEnvProcessEnv(t *testing.T) {
	skipIfPackMissing(t, "frameworks")
	result := guard.Evaluate("rails db:reset", guard.WithEnv([]string{"RAILS_ENV=production"}))
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
	if result.DestructiveAssessment == nil || result.DestructiveAssessment.Severity != guard.Critical {
		t.Fatalf("assessment = %#v, want critical", result.DestructiveAssessment)
	}
	if len(result.Matches) > 0 && !result.Matches[0].EnvEscalated {
		t.Fatalf("expected env escalation on first match")
	}
}

func TestIntegrationWithPacksIncludeList(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	result := guard.Evaluate("rm -rf /", guard.WithPacks("core.git"))
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestIntegrationWithPacksEmptyList(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithPacks())
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestIntegrationMultiMatchCompound(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	skipIfPackMissing(t, "core.filesystem")
	result := guard.Evaluate("git push --force && rm -rf /")
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
	if len(result.Matches) < 2 {
		t.Fatalf("matches len = %d, want >=2", len(result.Matches))
	}
	if result.DestructiveAssessment == nil || result.DestructiveAssessment.Severity != guard.Critical {
		t.Fatalf("assessment = %#v, want critical", result.DestructiveAssessment)
	}
}

func TestIntegrationEnabledAndDisabledPacks(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	skipIfPackMissing(t, "core.filesystem")
	result := guard.Evaluate(
		"git push --force",
		guard.WithPacks("core.git", "core.filesystem"),
		guard.WithDisabledPacks("core.git"),
	)
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestIntegrationResultFieldsPopulated(t *testing.T) {
	skipIfPackMissing(t, "core.git")
	command := "git push --force origin main"
	result := guard.Evaluate(command)
	if result.Command != command {
		t.Fatalf("command = %q, want %q", result.Command, command)
	}
	if result.Decision != guard.Allow {
		if result.DestructiveAssessment == nil {
			t.Fatal("assessment must be non-nil for non-Allow decisions")
		}
		if len(result.Matches) == 0 {
			t.Fatal("matches must be non-empty for non-Allow decisions")
		}
		m := result.Matches[0]
		if m.Pack == "" || m.Rule == "" || m.Reason == "" {
			t.Fatalf("incomplete match: %+v", m)
		}
	}
}

func TestIntegrationUnknownPackWarning(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithPacks("does.not.exist"))
	found := false
	for _, w := range result.Warnings {
		if w.Code == guard.WarnUnknownPackID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected WarnUnknownPackID warning")
	}
}

func TestIntegrationGoldenFileCorpus(t *testing.T) {
	entries := loadGoldenEntries(t, filepath.Join("testdata", "golden", "commands.txt"))
	for _, e := range entries {
		t.Run(e.command, func(t *testing.T) {
			result := guard.Evaluate(e.command, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			if result.Decision.String() != e.decision {
				t.Fatalf("decision=%s want=%s command=%q", result.Decision.String(), e.decision, e.command)
			}
		})
	}
}

func skipIfPackMissing(t *testing.T, packID string) {
	t.Helper()
	for _, p := range guard.Packs() {
		if p.ID == packID {
			return
		}
	}
	t.Skipf("pack %s not registered", packID)
}

type goldenEntry struct {
	decision string
	command  string
}

func loadGoldenEntries(t *testing.T, path string) []goldenEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden file: %v", err)
	}
	defer f.Close()

	var out []goldenEntry
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		txt := strings.TrimSpace(sc.Text())
		if txt == "" || strings.HasPrefix(txt, "#") {
			continue
		}
		parts := strings.SplitN(txt, "|", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid golden entry at line %d: %q", line, txt)
		}
		out = append(out, goldenEntry{
			decision: strings.TrimSpace(parts[0]),
			command:  strings.TrimSpace(parts[1]),
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan golden file: %v", err)
	}
	return out
}
