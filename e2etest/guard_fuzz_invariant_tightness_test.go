package e2etest

import (
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

func TestPropertyFuzzInvariantsTightness(t *testing.T) {
	long := strings.Repeat("a", parse.MaxInputSize+1)
	broken := []struct {
		name    string
		command string
		result  guard.Result
	}{
		{
			name:    "inv1-invalid-decision",
			command: "echo hello",
			result:  guard.Result{Decision: guard.Decision(99), Command: "echo hello"},
		},
		{
			name:    "inv2-command-not-preserved",
			command: "echo hello",
			result:  guard.Result{Decision: guard.Allow, Command: "different"},
		},
		{
			name:    "inv3-empty-command-non-allow",
			command: "",
			result:  guard.Result{Decision: guard.Deny, Command: ""},
		},
		{
			name:    "inv4-nil-assessment-with-deny",
			command: "echo hello",
			result:  guard.Result{Decision: guard.Deny, Command: "echo hello"},
		},
		{
			name:    "inv5-matches-without-assessment",
			command: "echo hello",
			result: guard.Result{
				Decision: guard.Deny,
				Command:  "echo hello",
				Matches:  []guard.Match{{Pack: "test.pack", Rule: "test-rule"}},
			},
		},
		{
			name:    "inv6-invalid-assessment-severity",
			command: "echo hello",
			result: guard.Result{
				Decision:   guard.Deny,
				Command:    "echo hello",
				Assessment: &guard.Assessment{Severity: guard.Severity(99), Confidence: guard.ConfidenceHigh},
				Matches:    []guard.Match{{Pack: "test.pack", Rule: "test-rule"}},
			},
		},
		{
			name:    "inv7-empty-match-pack",
			command: "echo hello",
			result: guard.Result{
				Decision:   guard.Deny,
				Command:    "echo hello",
				Assessment: &guard.Assessment{Severity: guard.High, Confidence: guard.ConfidenceHigh},
				Matches:    []guard.Match{{Pack: "", Rule: ""}},
			},
		},
		{
			name:    "inv8-oversized-high-severity",
			command: long,
			result: guard.Result{
				Decision:   guard.Deny,
				Command:    long,
				Assessment: &guard.Assessment{Severity: guard.High, Confidence: guard.ConfidenceHigh},
				Matches:    []guard.Match{{Pack: "test.pack", Rule: "test-rule"}},
			},
		},
	}

	for _, tc := range broken {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if violation := guardFirstInvariantViolation(tc.command, tc.result); violation == "" {
				t.Fatalf("expected an invariant violation for %s", tc.name)
			}
		})
	}
}

func guardFirstInvariantViolation(command string, result guard.Result) string {
	// INV-1: decision must be valid enum.
	switch result.Decision {
	case guard.Allow, guard.Deny, guard.Ask:
	default:
		return "INV-1"
	}

	// INV-2: command preserved.
	if result.Command != command {
		return "INV-2"
	}

	// INV-3: empty/whitespace command allows.
	if strings.TrimSpace(command) == "" && result.Decision != guard.Allow {
		return "INV-3"
	}

	// INV-4: nil assessment implies allow and no matches.
	if result.Assessment == nil {
		if result.Decision != guard.Allow || len(result.Matches) > 0 {
			return "INV-4"
		}
	}

	// INV-5: matches implies assessment.
	if len(result.Matches) > 0 && result.Assessment == nil {
		return "INV-5"
	}

	// INV-6: assessment severity valid.
	if result.Assessment != nil {
		switch result.Assessment.Severity {
		case guard.Indeterminate, guard.Low, guard.Medium, guard.High, guard.Critical:
		default:
			return "INV-6"
		}
	}

	// INV-7: match fields populated.
	for _, m := range result.Matches {
		if m.Pack == "" {
			return "INV-7"
		}
		if m.Rule == "" && m.Pack != "_blocklist" && m.Pack != "_allowlist" {
			return "INV-7"
		}
	}

	// INV-8: oversized input should not escalate beyond indeterminate.
	if len(command) > parse.MaxInputSize && result.Assessment != nil && result.Assessment.Severity > guard.Indeterminate {
		return "INV-8"
	}

	return ""
}
