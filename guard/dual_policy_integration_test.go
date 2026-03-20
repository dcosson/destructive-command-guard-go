package guard_test

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestDualPolicyDestructivePermissivePrivacyStrict(t *testing.T) {
	// Destructive command with permissive destructive policy should not be denied.
	result := guard.Evaluate("git push --force",
		guard.WithDestructivePolicy(guard.PermissivePolicy()),
		guard.WithPrivacyPolicy(guard.StrictPolicy()),
	)
	if len(result.Matches) > 0 {
		// git push --force is High severity → permissive asks on High
		if result.Decision == guard.Deny {
			t.Fatalf("destructive-permissive should not Deny high-severity destructive command, got %v", result.Decision)
		}
	}
}

func TestDualPolicyBothCategoriesStrictestWins(t *testing.T) {
	// When both policies are applied to a "both" category command,
	// the strictest decision should win (deny > ask > allow).
	result := guard.Evaluate("csrutil disable",
		guard.WithDestructivePolicy(guard.PermissivePolicy()),
		guard.WithPrivacyPolicy(guard.StrictPolicy()),
		guard.WithPacks("macos.system"),
	)
	if len(result.Matches) > 0 {
		// csrutil-disable is CategoryBoth + Critical severity
		// Permissive denies Critical, Strict denies Critical → Deny
		if result.Decision != guard.Deny {
			t.Fatalf("both-category critical command should be Deny, got %v", result.Decision)
		}
	}
}

func TestDualPolicyDefaultBothInteractive(t *testing.T) {
	// Default policies (both interactive) should behave like old single policy.
	result := guard.Evaluate("git push --force")
	if len(result.Matches) > 0 {
		// High severity + interactive → Deny
		if result.Decision != guard.Deny {
			t.Fatalf("default dual-interactive should Deny high-severity, got %v", result.Decision)
		}
	}
}

func TestDualPolicyMatchCategories(t *testing.T) {
	// Verify that matches carry the correct Category field.
	result := guard.Evaluate("git push --force")
	for _, m := range result.Matches {
		if m.Category == 0 {
			t.Fatalf("match %s.%s has zero category (should be normalized)", m.Pack, m.Rule)
		}
		if !m.Category.HasDestructive() {
			t.Fatalf("git push --force match should be destructive, got %s", m.Category)
		}
	}
}

func TestDualPolicyResultAssessments(t *testing.T) {
	// Destructive command should only populate DestructiveAssessment.
	result := guard.Evaluate("rm -rf /")
	if result.DestructiveAssessment == nil && len(result.Matches) > 0 {
		t.Fatal("expected DestructiveAssessment for rm -rf /")
	}
	if result.PrivacyAssessment != nil {
		t.Fatalf("unexpected PrivacyAssessment for purely destructive command: %+v", result.PrivacyAssessment)
	}
}

func TestDualPolicyBlocklistBypassesDualPolicy(t *testing.T) {
	// Blocklist should bypass the dual-policy pipeline entirely.
	result := guard.Evaluate("echo hello",
		guard.WithBlocklist("echo *"),
		guard.WithDestructivePolicy(guard.PermissivePolicy()),
		guard.WithPrivacyPolicy(guard.PermissivePolicy()),
	)
	if result.Decision != guard.Deny {
		t.Fatalf("blocklist should Deny regardless of policy, got %v", result.Decision)
	}
}

func TestDualPolicyAllowlistBypassesDualPolicy(t *testing.T) {
	// Allowlist should bypass the dual-policy pipeline entirely.
	result := guard.Evaluate("git push --force",
		guard.WithAllowlist("git push *"),
		guard.WithDestructivePolicy(guard.StrictPolicy()),
		guard.WithPrivacyPolicy(guard.StrictPolicy()),
	)
	if result.Decision != guard.Allow {
		t.Fatalf("allowlist should Allow regardless of policy, got %v", result.Decision)
	}
}
