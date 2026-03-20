package integration

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestIntegrationRealWorldScenarios(t *testing.T) {
	scenarios := []struct {
		name         string
		command      string
		wantDecision guard.Decision
		wantMinSev   guard.Severity
	}{
		{"force push to main", "git push --force origin main", guard.Ask, guard.High},
		{"production database reset", "RAILS_ENV=production rails db:reset", guard.Deny, guard.Critical},
		{"dev database reset", "RAILS_ENV=development rails db:reset", guard.Ask, guard.High},
		{"variable carries danger", "DIR=/tmp/e2e; rm -rf $DIR", guard.Deny, guard.Critical},
		{"compound git + rm", "git push --force && rm -rf /tmp/e2e", guard.Deny, guard.Critical},
		{"grep dangerous pattern", `grep -r "DROP TABLE" .`, guard.Allow, guard.Indeterminate},
		{"man page safe", "man git-push", guard.Allow, guard.Indeterminate},
		{"empty command", "", guard.Allow, guard.Indeterminate},
		{"whitespace command", "   \t\n  ", guard.Allow, guard.Indeterminate},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			result := guard.Evaluate(sc.command, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			if result.Decision != sc.wantDecision {
				t.Fatalf("decision mismatch for %q: got %s want %s", sc.command, result.Decision, sc.wantDecision)
			}
			if sc.wantDecision != guard.Allow && result.DestructiveAssessment != nil &&
				result.DestructiveAssessment.Severity < sc.wantMinSev {
				t.Fatalf("severity too low for %q: got %s want >= %s", sc.command, result.DestructiveAssessment.Severity, sc.wantMinSev)
			}
		})
	}
}

func TestIntegrationPolicyVariations(t *testing.T) {
	type expectation struct {
		policy guard.Policy
		name   string
		want   guard.Decision
	}
	scenarios := []struct {
		name string
		cmd  string
		exps []expectation
	}{
		{
			name: "high severity command",
			cmd:  "git push --force origin main",
			exps: []expectation{
				{guard.StrictPolicy(), "strict", guard.Deny},
				{guard.InteractivePolicy(), "interactive", guard.Ask},
				{guard.PermissivePolicy(), "permissive", guard.Allow},
			},
		},
		{
			name: "critical severity command",
			cmd:  "rm -rf /tmp/e2e",
			exps: []expectation{
				{guard.StrictPolicy(), "strict", guard.Deny},
				{guard.InteractivePolicy(), "interactive", guard.Deny},
				{guard.PermissivePolicy(), "permissive", guard.Deny},
			},
		},
		{
			name: "safe command",
			cmd:  "echo hello",
			exps: []expectation{
				{guard.StrictPolicy(), "strict", guard.Allow},
				{guard.InteractivePolicy(), "interactive", guard.Allow},
				{guard.PermissivePolicy(), "permissive", guard.Allow},
			},
		},
	}

	for _, sc := range scenarios {
		for _, exp := range sc.exps {
			sc := sc
			exp := exp
			t.Run(sc.name+"/"+exp.name, func(t *testing.T) {
				result := guard.Evaluate(sc.cmd, guard.WithDestructivePolicy(exp.policy))
				if result.Decision != exp.want {
					t.Fatalf("policy %s mismatch for %q: got %s want %s", exp.name, sc.cmd, result.Decision, exp.want)
				}
			})
		}
	}
}
