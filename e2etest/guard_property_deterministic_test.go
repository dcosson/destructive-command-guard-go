package e2etest

import (
	"slices"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestPropertyEvaluateDeterminismWithOptions(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		opts []guard.Option
	}{
		{name: "empty", cmd: "", opts: nil},
		{name: "whitespace", cmd: "   ", opts: []guard.Option{guard.WithPolicy(guard.StrictPolicy())}},
		{name: "allowlist+blocklist", cmd: "git push --force", opts: []guard.Option{guard.WithAllowlist("*"), guard.WithBlocklist("git push *")}},
		{name: "disabled packs", cmd: "git push --force", opts: allPacksDisabledOpts()},
		{name: "enabled unknown", cmd: "echo hello", opts: []guard.Option{guard.WithPacks("does.not.exist")}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r1 := guard.Evaluate(tc.cmd, tc.opts...)
			r2 := guard.Evaluate(tc.cmd, tc.opts...)

			if r1.Decision != r2.Decision {
				t.Fatalf("decision mismatch: %s vs %s", r1.Decision, r2.Decision)
			}
			if r1.Command != r2.Command {
				t.Fatalf("command mismatch: %q vs %q", r1.Command, r2.Command)
			}
			if len(r1.Matches) != len(r2.Matches) {
				t.Fatalf("matches len mismatch: %d vs %d", len(r1.Matches), len(r2.Matches))
			}
			if len(r1.Warnings) != len(r2.Warnings) {
				t.Fatalf("warnings len mismatch: %d vs %d", len(r1.Warnings), len(r2.Warnings))
			}
			if (r1.Assessment == nil) != (r2.Assessment == nil) {
				t.Fatalf("assessment nil mismatch: %#v vs %#v", r1.Assessment, r2.Assessment)
			}
			if r1.Assessment != nil {
				if *r1.Assessment != *r2.Assessment {
					t.Fatalf("assessment mismatch: %#v vs %#v", *r1.Assessment, *r2.Assessment)
				}
			}
		})
	}
}

func TestPropertyResultCommandPreserved(t *testing.T) {
	commands := []string{"", "   ", "git status", "rm -rf / && echo done", "命令", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, cmd := range commands {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if res.Command != cmd {
			t.Fatalf("result command=%q want=%q", res.Command, cmd)
		}
	}
}

func TestPropertyAssessmentMatchConsistency(t *testing.T) {
	commands := []string{"git push --force", "git status", "echo hello", "rm -rf /"}
	for _, cmd := range commands {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if len(res.Matches) > 0 && res.Assessment == nil {
			t.Fatalf("matches present but assessment nil for %q", cmd)
		}
		if res.Assessment == nil {
			if res.Decision != guard.Allow {
				t.Fatalf("nil assessment must imply allow for %q; got %s", cmd, res.Decision)
			}
			if len(res.Matches) != 0 {
				t.Fatalf("nil assessment must imply no matches for %q; got %d", cmd, len(res.Matches))
			}
		}
	}
}

func TestPropertyAllPacksDisabledAllowsNonBlocklisted(t *testing.T) {
	opts := allPacksDisabledOpts()
	if len(opts) == 0 {
		t.Skip("no packs registered")
	}
	for _, cmd := range []string{"git push --force", "rm -rf /", "rails db:reset", "echo hello"} {
		res := guard.Evaluate(cmd, opts...)
		if res.Decision != guard.Allow {
			t.Fatalf("all packs disabled should allow %q, got %s", cmd, res.Decision)
		}
	}

	blocked := guard.Evaluate("echo hello", append(opts, guard.WithBlocklist("echo *"))...)
	if blocked.Decision != guard.Deny {
		t.Fatalf("blocklist should still deny with all packs disabled; got %s", blocked.Decision)
	}
}

func TestDeterministicPolicyMonotonicityMatrix(t *testing.T) {
	severities := []guard.Severity{guard.Indeterminate, guard.Low, guard.Medium, guard.High, guard.Critical}
	ord := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}
	strict := guard.StrictPolicy()
	inter := guard.InteractivePolicy()
	perm := guard.PermissivePolicy()
	for _, sev := range severities {
		a := guard.Assessment{Severity: sev, Confidence: guard.ConfidenceHigh}
		sd, id, pd := strict.Decide(a), inter.Decide(a), perm.Decide(a)
		if ord[id] < ord[pd] {
			t.Fatalf("interactive less strict than permissive for %s: inter=%s perm=%s", sev, id, pd)
		}
		if ord[sd] < ord[id] {
			t.Fatalf("strict less strict than interactive for %s: strict=%s inter=%s", sev, sd, id)
		}
	}
}

func TestDeterministicPacksMetadataCopyIsolation(t *testing.T) {
	p1 := guard.Packs()
	if len(p1) == 0 {
		t.Fatal("expected at least one pack")
	}
	p2 := guard.Packs()
	if len(p1) != len(p2) {
		t.Fatalf("packs length changed across calls: %d vs %d", len(p1), len(p2))
	}
	if !slices.EqualFunc(p1, p2, func(a, b guard.PackInfo) bool { return a.ID == b.ID && a.Name == b.Name && a.SafeCount == b.SafeCount && a.DestrCount == b.DestrCount }) {
		t.Fatalf("pack metadata changed across sequential calls")
	}
	if len(p1[0].Keywords) > 0 {
		orig := p1[0].Keywords[0]
		p1[0].Keywords[0] = "MUTATED"
		again := guard.Packs()
		if len(again[0].Keywords) > 0 && again[0].Keywords[0] != orig {
			t.Fatalf("pack keywords alias leaked; expected %q got %q", orig, again[0].Keywords[0])
		}
	}
}

func allPacksDisabledOpts() []guard.Option {
	all := guard.Packs()
	if len(all) == 0 {
		return nil
	}
	ids := make([]string, len(all))
	for i, p := range all {
		ids[i] = p.ID
	}
	return []guard.Option{guard.WithDisabledPacks(ids...)}
}
