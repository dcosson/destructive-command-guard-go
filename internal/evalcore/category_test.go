package evalcore

import "testing"

func TestRuleCategoryBitmask(t *testing.T) {
	if CategoryDestructive != 1 {
		t.Fatalf("CategoryDestructive = %d, want 1", CategoryDestructive)
	}
	if CategoryPrivacy != 2 {
		t.Fatalf("CategoryPrivacy = %d, want 2", CategoryPrivacy)
	}
	if CategoryBoth != 3 {
		t.Fatalf("CategoryBoth = %d, want 3", CategoryBoth)
	}
	if CategoryBoth != CategoryDestructive|CategoryPrivacy {
		t.Fatal("CategoryBoth != CategoryDestructive|CategoryPrivacy")
	}
}

func TestRuleCategoryHas(t *testing.T) {
	cases := []struct {
		cat            RuleCategory
		hasDestructive bool
		hasPrivacy     bool
	}{
		{CategoryDestructive, true, false},
		{CategoryPrivacy, false, true},
		{CategoryBoth, true, true},
		{0, false, false},
	}
	for _, tc := range cases {
		if got := tc.cat.HasDestructive(); got != tc.hasDestructive {
			t.Errorf("%s.HasDestructive() = %v, want %v", tc.cat, got, tc.hasDestructive)
		}
		if got := tc.cat.HasPrivacy(); got != tc.hasPrivacy {
			t.Errorf("%s.HasPrivacy() = %v, want %v", tc.cat, got, tc.hasPrivacy)
		}
	}
}

func TestRuleCategoryString(t *testing.T) {
	cases := []struct {
		cat  RuleCategory
		want string
	}{
		{CategoryDestructive, "Destructive"},
		{CategoryPrivacy, "Privacy"},
		{CategoryBoth, "Both"},
		{0, "Unknown"},
		{RuleCategory(99), "Unknown"},
	}
	for _, tc := range cases {
		if got := tc.cat.String(); got != tc.want {
			t.Errorf("RuleCategory(%d).String() = %q, want %q", tc.cat, got, tc.want)
		}
	}
}

func TestPolicyConfigDecide(t *testing.T) {
	cases := []struct {
		name        string
		dPolicy     Policy
		pPolicy     Policy
		destructive *Assessment
		privacy     *Assessment
		want        Decision
	}{
		{
			name:    "both-nil-allow",
			dPolicy: InteractivePolicy(),
			pPolicy: InteractivePolicy(),
			want:    Allow,
		},
		{
			name:        "destructive-only-deny",
			dPolicy:     StrictPolicy(),
			pPolicy:     PermissivePolicy(),
			destructive: &Assessment{Severity: Medium, Confidence: ConfidenceHigh},
			want:        Deny,
		},
		{
			name:    "privacy-only-deny",
			dPolicy: PermissivePolicy(),
			pPolicy: StrictPolicy(),
			privacy: &Assessment{Severity: Medium, Confidence: ConfidenceHigh},
			want:    Deny,
		},
		{
			name:        "both-present-deny-wins",
			dPolicy:     PermissivePolicy(),
			pPolicy:     StrictPolicy(),
			destructive: &Assessment{Severity: Low, Confidence: ConfidenceHigh},
			privacy:     &Assessment{Severity: Medium, Confidence: ConfidenceHigh},
			want:        Deny, // privacy strict denies medium
		},
		{
			name:        "both-present-ask-wins-over-allow",
			dPolicy:     InteractivePolicy(),
			pPolicy:     PermissivePolicy(),
			destructive: &Assessment{Severity: Medium, Confidence: ConfidenceHigh},
			privacy:     &Assessment{Severity: Low, Confidence: ConfidenceHigh},
			want:        Ask, // destructive interactive asks on medium
		},
		{
			name:        "both-allow",
			dPolicy:     PermissivePolicy(),
			pPolicy:     PermissivePolicy(),
			destructive: &Assessment{Severity: Low, Confidence: ConfidenceHigh},
			privacy:     &Assessment{Severity: Low, Confidence: ConfidenceHigh},
			want:        Allow,
		},
		{
			name:        "deny-trumps-ask",
			dPolicy:     InteractivePolicy(),
			pPolicy:     StrictPolicy(),
			destructive: &Assessment{Severity: Medium, Confidence: ConfidenceHigh}, // Ask
			privacy:     &Assessment{Severity: Medium, Confidence: ConfidenceHigh}, // Deny
			want:        Deny,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pc := PolicyConfig{
				DestructivePolicy: tc.dPolicy,
				PrivacyPolicy:     tc.pPolicy,
			}
			got := pc.Decide(tc.destructive, tc.privacy)
			if got != tc.want {
				t.Fatalf("Decide() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestZeroCategoryNotDestructiveOrPrivacy(t *testing.T) {
	// A zero RuleCategory must not match either HasDestructive or HasPrivacy.
	// This verifies that the normalization logic in the pipeline is required.
	var zero RuleCategory
	if zero.HasDestructive() {
		t.Fatal("zero category should not HasDestructive()")
	}
	if zero.HasPrivacy() {
		t.Fatal("zero category should not HasPrivacy()")
	}
}
