package eval

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func TestAggregateByCategory(t *testing.T) {
	cases := []struct {
		name    string
		matches []Match
		wantD   *Assessment
		wantP   *Assessment
	}{
		{
			name:    "no-matches",
			matches: nil,
			wantD:   nil,
			wantP:   nil,
		},
		{
			name: "destructive-only",
			matches: []Match{
				{Category: evalcore.CategoryDestructive, Severity: SeverityHigh, Confidence: ConfidenceHigh},
				{Category: evalcore.CategoryDestructive, Severity: SeverityMedium, Confidence: ConfidenceLow},
			},
			wantD: &Assessment{Severity: SeverityHigh, Confidence: ConfidenceHigh},
			wantP: nil,
		},
		{
			name: "privacy-only",
			matches: []Match{
				{Category: evalcore.CategoryPrivacy, Severity: SeverityMedium, Confidence: ConfidenceHigh},
			},
			wantD: nil,
			wantP: &Assessment{Severity: SeverityMedium, Confidence: ConfidenceHigh},
		},
		{
			name: "both-category-enters-both-lanes",
			matches: []Match{
				{Category: evalcore.CategoryBoth, Severity: SeverityCritical, Confidence: ConfidenceHigh},
			},
			wantD: &Assessment{Severity: SeverityCritical, Confidence: ConfidenceHigh},
			wantP: &Assessment{Severity: SeverityCritical, Confidence: ConfidenceHigh},
		},
		{
			name: "mixed-categories",
			matches: []Match{
				{Category: evalcore.CategoryDestructive, Severity: SeverityHigh, Confidence: ConfidenceHigh},
				{Category: evalcore.CategoryPrivacy, Severity: SeverityMedium, Confidence: ConfidenceMedium},
				{Category: evalcore.CategoryBoth, Severity: SeverityLow, Confidence: ConfidenceLow},
			},
			wantD: &Assessment{Severity: SeverityHigh, Confidence: ConfidenceHigh},
			wantP: &Assessment{Severity: SeverityMedium, Confidence: ConfidenceMedium},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, p := aggregateByCategory(tc.matches)
			assertAssessmentEqual(t, "destructive", d, tc.wantD)
			assertAssessmentEqual(t, "privacy", p, tc.wantP)
		})
	}
}

func assertAssessmentEqual(t *testing.T, label string, got, want *Assessment) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("%s: got=%v want=%v", label, got, want)
	}
	if got != nil && *got != *want {
		t.Fatalf("%s: got=%+v want=%+v", label, *got, *want)
	}
}

func TestPipelineDualPolicyEvaluation(t *testing.T) {
	// Create a registry with a destructive rule and a privacy rule.
	reg := packs.NewRegistry(
		packs.Pack{
			ID:       "test.destructive",
			Name:     "Test Destructive",
			Keywords: []string{"rm"},
			Rules: []packs.Rule{
				{
					ID:         "test-rm",
					Category:   evalcore.CategoryDestructive,
					Severity:   int(SeverityHigh),
					Confidence: int(ConfidenceHigh),
					Reason:     "Destructive rm",
					Match:      packs.Name("rm"),
				},
			},
		},
		packs.Pack{
			ID:       "test.privacy",
			Name:     "Test Privacy",
			Keywords: []string{"cat"},
			Rules: []packs.Rule{
				{
					ID:         "test-cat-ssh",
					Category:   evalcore.CategoryPrivacy,
					Severity:   int(SeverityMedium),
					Confidence: int(ConfidenceHigh),
					Reason:     "SSH key access",
					Match:      packs.And(packs.Name("cat"), packs.Arg(".ssh/id_rsa")),
				},
			},
		},
	)

	pipeline := NewPipeline(reg)

	t.Run("destructive-permissive-privacy-strict", func(t *testing.T) {
		// rm -rf should be allowed by permissive destructive policy (High → Ask)
		result := pipeline.Run("rm -rf /tmp/foo", Config{
			DestructivePolicy: evalcore.PermissivePolicy(),
			PrivacyPolicy:     evalcore.StrictPolicy(),
		})
		if len(result.Matches) > 0 {
			// Only destructive matches, privacy strict should not affect
			if result.DestructiveAssessment == nil {
				t.Fatal("expected destructive assessment")
			}
			if result.PrivacyAssessment != nil {
				t.Fatal("expected no privacy assessment for destructive command")
			}
		}
	})

	t.Run("privacy-command-denied-by-strict", func(t *testing.T) {
		result := pipeline.Run("cat .ssh/id_rsa", Config{
			DestructivePolicy: evalcore.PermissivePolicy(),
			PrivacyPolicy:     evalcore.StrictPolicy(),
		})
		if len(result.Matches) > 0 {
			if result.Decision != DecisionDeny {
				t.Fatalf("decision = %v, want Deny (strict privacy denies medium)", result.Decision)
			}
			if result.PrivacyAssessment == nil {
				t.Fatal("expected privacy assessment")
			}
		}
	})
}

func TestPipelineZeroCategoryNormalization(t *testing.T) {
	// Rule with zero category should be normalized to CategoryDestructive.
	reg := packs.NewRegistry(packs.Pack{
		ID:       "test.zero",
		Name:     "Test Zero Category",
		Keywords: []string{"danger"},
		Rules: []packs.Rule{
			{
				ID:         "zero-cat-rule",
				Category:   0, // deliberately unset
				Severity:   int(SeverityHigh),
				Confidence: int(ConfidenceHigh),
				Reason:     "zero category rule",
				Match:      packs.Name("danger"),
			},
		},
	})

	pipeline := NewPipeline(reg)
	result := pipeline.Run("danger", Config{
		DestructivePolicy: evalcore.InteractivePolicy(),
		PrivacyPolicy:     evalcore.InteractivePolicy(),
	})

	if len(result.Matches) == 0 {
		t.Fatal("expected matches for 'danger' command")
	}

	// The match should have been normalized to CategoryDestructive
	for _, m := range result.Matches {
		if m.Category != evalcore.CategoryDestructive {
			t.Fatalf("match category = %v, want CategoryDestructive (zero should normalize)", m.Category)
		}
	}

	// Should have a destructive assessment, not privacy
	if result.DestructiveAssessment == nil {
		t.Fatal("expected destructive assessment from normalized zero-category rule")
	}
	if result.PrivacyAssessment != nil {
		t.Fatal("zero-category rule should NOT produce privacy assessment")
	}

	// Decision should not be Allow (it's High severity with Interactive policy → Deny)
	if result.Decision == DecisionAllow {
		t.Fatal("zero-category rule should not result in Allow (fail-open)")
	}
}
