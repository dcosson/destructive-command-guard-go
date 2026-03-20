package guard_test

import (
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestEvaluateEmptyCommand(t *testing.T) {
	result := guard.Evaluate("")
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
	if result.DestructiveAssessment != nil {
		t.Fatalf("destructive assessment = %#v, want nil", result.DestructiveAssessment)
	}
	if result.PrivacyAssessment != nil {
		t.Fatalf("privacy assessment = %#v, want nil", result.PrivacyAssessment)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("matches len = %d, want 0", len(result.Matches))
	}
}

func TestEvaluateWhitespaceCommand(t *testing.T) {
	result := guard.Evaluate("   ")
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestEvaluateSafeCommand(t *testing.T) {
	result := guard.Evaluate("git status")
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestEvaluateDestructiveCommand(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithDestructivePolicy(guard.InteractivePolicy()))
	if len(result.Matches) > 0 {
		if result.Decision == guard.Allow {
			t.Fatalf("decision = %v, want non-Allow", result.Decision)
		}
		if result.Matches[0].Pack != "core.git" {
			t.Fatalf("pack = %q, want core.git", result.Matches[0].Pack)
		}
	}
}

func TestEvaluateWithStrictPolicy(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithDestructivePolicy(guard.StrictPolicy()))
	if len(result.Matches) > 0 && result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
}

func TestEvaluateWithPermissivePolicy(t *testing.T) {
	// git push --force is High severity; permissive allows up to High.
	result := guard.Evaluate("git push --force", guard.WithDestructivePolicy(guard.PermissivePolicy()))
	if len(result.Matches) > 0 && result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestEvaluateWithAllowlist(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithAllowlist("git push *"))
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestEvaluateWithBlocklist(t *testing.T) {
	result := guard.Evaluate("echo hello", guard.WithBlocklist("echo *"))
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
}

func TestBlocklistOverridesAllowlist(t *testing.T) {
	result := guard.Evaluate(
		"git push --force",
		guard.WithAllowlist("git *"),
		guard.WithBlocklist("git push --force*"),
	)
	if result.Decision != guard.Deny {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Deny)
	}
}

func TestEvaluateWithDisabledPacks(t *testing.T) {
	all := guard.Packs()
	ids := make([]string, len(all))
	for i, p := range all {
		ids[i] = p.ID
	}
	result := guard.Evaluate("git push --force", guard.WithDisabledPacks(ids...))
	if result.Decision != guard.Allow {
		t.Fatalf("decision = %v, want %v", result.Decision, guard.Allow)
	}
}

func TestEvaluateConcurrentSafety(t *testing.T) {
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			_ = guard.Evaluate("git push --force", guard.WithDestructivePolicy(guard.InteractivePolicy()))
		}()
	}
	wg.Wait()
}

func TestEvaluateWithNilPolicyOptionDoesNotPanic(t *testing.T) {
	result := guard.Evaluate("git push --force", guard.WithDestructivePolicy(nil))
	if len(result.Matches) > 0 && result.Decision == guard.Allow {
		t.Fatalf("decision = %v, want non-Allow when matches exist", result.Decision)
	}
}

func TestPacksMetadata(t *testing.T) {
	packs := guard.Packs()
	if len(packs) == 0 {
		t.Fatal("expected at least one pack")
	}
	for _, p := range packs {
		if p.ID == "" {
			t.Fatal("pack ID must be non-empty")
		}
		if p.Name == "" {
			t.Fatal("pack Name must be non-empty")
		}
	}
}
