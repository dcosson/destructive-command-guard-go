package external

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// policyByName returns a guard.Policy for the given policy name string.
func policyByName(name string) guard.Policy {
	switch name {
	case "allow-all":
		return guard.AllowAllPolicy()
	case "permissive":
		return guard.PermissivePolicy()
	case "moderate":
		return guard.ModeratePolicy()
	case "strict":
		return guard.StrictPolicy()
	case "block-all":
		return guard.BlockAllPolicy()
	case "interactive":
		return guard.InteractivePolicy()
	default:
		panic("unknown policy: " + name)
	}
}

func TestLibrarySafeCommands(t *testing.T) {
	for _, cmd := range SafeCommands {
		t.Run(cmd, func(t *testing.T) {
			result := guard.Evaluate(cmd)
			if result.Decision != guard.Allow {
				t.Fatalf("expected Allow, got %s for %q", result.Decision, cmd)
			}
		})
	}
}

func TestLibraryDefaultPolicy(t *testing.T) {
	for _, tc := range DefaultPolicyCases {
		t.Run(tc.Name, func(t *testing.T) {
			result := guard.Evaluate(tc.Command)
			got := result.Decision.String()
			if got != tc.WantDecision {
				t.Fatalf("decision=%s want=%s for %q", got, tc.WantDecision, tc.Command)
			}
		})
	}
}

func TestLibraryPolicyVariations(t *testing.T) {
	for _, tc := range PolicyVariations {
		t.Run(tc.Policy, func(t *testing.T) {
			p := policyByName(tc.Policy)
			result := guard.Evaluate(PolicyVariationCommand,
				guard.WithDestructivePolicy(p),
			)
			got := result.Decision.String()
			if got != tc.WantDecision {
				t.Fatalf("decision=%s want=%s for policy=%s", got, tc.WantDecision, tc.Policy)
			}
		})
	}
}

func TestLibraryDualPolicySplit(t *testing.T) {
	t.Run("destructive-allowed-by-permissive", func(t *testing.T) {
		result := guard.Evaluate("git push --force",
			guard.WithDestructivePolicy(guard.PermissivePolicy()),
			guard.WithPrivacyPolicy(guard.StrictPolicy()),
		)
		if result.Decision != guard.Allow {
			t.Fatalf("expected Allow for destructive-permissive, got %s", result.Decision)
		}
	})

	t.Run("critical-denied-even-permissive", func(t *testing.T) {
		result := guard.Evaluate("rm -rf /",
			guard.WithDestructivePolicy(guard.PermissivePolicy()),
			guard.WithPrivacyPolicy(guard.StrictPolicy()),
		)
		if result.Decision != guard.Deny {
			t.Fatalf("expected Deny for critical, got %s", result.Decision)
		}
	})
}

func TestLibraryPerCategoryAssessments(t *testing.T) {
	t.Run("destructive-only", func(t *testing.T) {
		result := guard.Evaluate("git push --force")
		if result.DestructiveAssessment == nil {
			t.Fatal("expected destructive assessment for destructive command")
		}
		if result.PrivacyAssessment != nil {
			t.Fatal("unexpected privacy assessment for destructive command")
		}
	})

	t.Run("both-assessments-populated", func(t *testing.T) {
		result := guard.Evaluate("rm -rf /")
		if result.DestructiveAssessment == nil {
			t.Fatal("expected destructive assessment")
		}
		if result.DestructiveAssessment.Severity != guard.Critical {
			t.Fatalf("expected Critical severity, got %s", result.DestructiveAssessment.Severity)
		}
	})
}

func TestLibraryPacksAndRules(t *testing.T) {
	t.Run("packs-non-empty", func(t *testing.T) {
		packs := guard.Packs()
		if len(packs) == 0 {
			t.Fatal("expected non-empty packs list")
		}
		found := false
		for _, p := range packs {
			if p.ID == "core.git" {
				found = true
				if p.Destructive.Count == 0 {
					t.Fatal("core.git should have destructive rules")
				}
				break
			}
		}
		if !found {
			t.Fatal("core.git pack not found")
		}
	})

	t.Run("rules-non-empty", func(t *testing.T) {
		rules := guard.Rules()
		if len(rules) < 100 {
			t.Fatalf("expected 100+ rules, got %d", len(rules))
		}
		// Verify we have all three categories represented.
		var hasDestructive, hasPrivacy, hasBoth bool
		for _, r := range rules {
			switch r.Category {
			case guard.CategoryDestructive:
				hasDestructive = true
			case guard.CategoryPrivacy:
				hasPrivacy = true
			case guard.CategoryBoth:
				hasBoth = true
			}
		}
		if !hasDestructive {
			t.Fatal("no destructive rules found")
		}
		if !hasPrivacy {
			t.Fatal("no privacy rules found")
		}
		if !hasBoth {
			t.Fatal("no both-category rules found")
		}
	})
}

func TestLibraryAllowlistBlocklist(t *testing.T) {
	t.Run("allowlist-bypasses-evaluation", func(t *testing.T) {
		result := guard.Evaluate("rm -rf /", guard.WithAllowlist("rm *"))
		if result.Decision != guard.Allow {
			t.Fatalf("expected Allow with allowlist, got %s", result.Decision)
		}
	})

	t.Run("blocklist-overrides-allowlist", func(t *testing.T) {
		result := guard.Evaluate("rm -rf /",
			guard.WithAllowlist("rm *"),
			guard.WithBlocklist("rm -rf *"),
		)
		if result.Decision != guard.Deny {
			t.Fatalf("expected Deny with blocklist, got %s", result.Decision)
		}
	})
}
