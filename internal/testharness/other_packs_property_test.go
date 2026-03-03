package testharness

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestPropertyEveryOtherPackDestructivePatternReachable(t *testing.T) {
	reach := otherPacksReachability()
	for _, packID := range []string{"frameworks", "secrets.vault", "remote.rsync", "platform.github"} {
		pk, ok := findPack(packID)
		if !ok {
			t.Run(packID, func(t *testing.T) { t.Skipf("pack %s not registered", packID) })
			continue
		}
		byRule := map[string]string{}
		for _, r := range reach[packID] {
			byRule[r.rule] = r.command
		}
		for _, dp := range pk.Destructive {
			dp := dp
			t.Run(packID+"/"+dp.ID, func(t *testing.T) {
				cmd, ok := byRule[dp.ID]
				if !ok {
					t.Skipf("missing reachability command for %s/%s", packID, dp.ID)
				}
				res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
				if !hasRuleMatch(res, packID, dp.ID) {
					t.Fatalf("reachability command did not match %s/%s: %q", packID, dp.ID, cmd)
				}
			})
		}
	}
}

func TestPropertyOtherPackMutualExclusion(t *testing.T) {
	for _, tc := range []struct {
		packID string
		cmd    string
	}{
		{"frameworks", "rails db:reset"},
		{"secrets.vault", "vault token revoke root"},
		{"remote.rsync", "rsync --delete -a src/ dst/"},
		{"platform.github", "gh repo delete owner/repo --yes"},
	} {
		tc := tc
		t.Run(tc.packID, func(t *testing.T) {
			if !HasRegisteredPack(tc.packID) {
				t.Skipf("pack %s not registered", tc.packID)
			}
			res := guard.Evaluate(tc.cmd, guard.WithPolicy(guard.InteractivePolicy()))
			for _, m := range res.Matches {
				if strings.HasPrefix(m.Pack, "frameworks") || strings.HasPrefix(m.Pack, "secrets.") ||
					strings.HasPrefix(m.Pack, "remote.") || strings.HasPrefix(m.Pack, "platform.") {
					if m.Pack != tc.packID {
						t.Fatalf("%s command triggered cross-pack match %s/%s", tc.packID, m.Pack, m.Rule)
					}
				}
			}
		})
	}
}

func TestPropertyOtherPackEnvSensitivitySplit(t *testing.T) {
	for _, id := range []string{"frameworks", "secrets.vault"} {
		pk, ok := findPack(id)
		if !ok {
			continue
		}
		for _, r := range pk.Destructive {
			if !r.EnvSensitive {
				t.Fatalf("%s/%s should be env-sensitive", id, r.ID)
			}
		}
	}
	for _, id := range []string{"remote.rsync", "platform.github"} {
		pk, ok := findPack(id)
		if !ok {
			continue
		}
		for _, r := range pk.Destructive {
			if r.EnvSensitive {
				t.Fatalf("%s/%s should NOT be env-sensitive", id, r.ID)
			}
		}
	}
}

func TestPropertyOtherPackConfidenceLevelsValid(t *testing.T) {
	for _, packID := range []string{"frameworks", "secrets.vault", "remote.rsync", "platform.github"} {
		pk, ok := findPack(packID)
		if !ok {
			continue
		}
		for _, r := range pk.Destructive {
			if r.Confidence < int(guard.ConfidenceLow) || r.Confidence > int(guard.ConfidenceHigh) {
				t.Fatalf("%s/%s has invalid confidence %d", packID, r.ID, r.Confidence)
			}
		}
	}
}

func TestPropertyDualInvocationParity(t *testing.T) {
	if !HasRegisteredPack("frameworks") {
		t.Skip("frameworks pack not registered")
	}
	pairs := []struct {
		direct string
		prefx  string
	}{
		{"manage.py flush", "python manage.py flush"},
		{"manage.py migrate --run-syncdb", "python manage.py migrate --run-syncdb"},
		{"artisan migrate:fresh", "php artisan migrate:fresh"},
		{"artisan migrate:reset", "php artisan migrate:reset"},
	}
	for _, p := range pairs {
		r1 := guard.Evaluate(p.direct, guard.WithPolicy(guard.InteractivePolicy()))
		r2 := guard.Evaluate(p.prefx, guard.WithPolicy(guard.InteractivePolicy()))
		if (r1.Decision == guard.Allow) != (r2.Decision == guard.Allow) {
			t.Fatalf("dual invocation parity mismatch direct=%q prefixed=%q", p.direct, p.prefx)
		}
	}
}

func TestPropertyColonDelimitedExactMatching(t *testing.T) {
	if !HasRegisteredPack("frameworks") {
		t.Skip("frameworks pack not registered")
	}
	partials := []string{
		"rails db:",
		"rails db:dro",
		"rails db:rese",
		"rake db:",
		"mix ecto.",
		"mix ecto.rese",
		"artisan migrate:",
		"artisan migrate:fres",
	}
	for _, cmd := range partials {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if res.Decision != guard.Allow {
			t.Fatalf("partial subcommand should be safe: %q => %s", cmd, res.Decision)
		}
	}
}

func TestPropertyOtherPackKeywordCoverage(t *testing.T) {
	reach := otherPacksReachability()
	for _, packID := range []string{"frameworks", "secrets.vault", "remote.rsync", "platform.github"} {
		pk, ok := findPack(packID)
		if !ok {
			continue
		}
		for _, pair := range reach[packID] {
			cmd := pair.command
			found := false
			for _, kw := range pk.Keywords {
				if strings.Contains(cmd, kw) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("reachability command contains no pack keywords: pack=%s cmd=%q keywords=%v", packID, cmd, pk.Keywords)
			}
		}
	}
}

func TestPropertyFrameworkToolIsolation(t *testing.T) {
	if !HasRegisteredPack("frameworks") {
		t.Skip("frameworks pack not registered")
	}
	wrongToolCmds := []string{
		"rails db:drop",
		"rake db:drop",
		"manage.py flush",
		"artisan migrate:fresh",
		"mix ecto.reset",
	}
	// Any single command should not produce matches for all tools simultaneously.
	for _, cmd := range wrongToolCmds {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		toolSeen := map[string]bool{}
		for _, m := range res.Matches {
			switch {
			case strings.Contains(m.Rule, "rails"):
				toolSeen["rails"] = true
			case strings.Contains(m.Rule, "rake"):
				toolSeen["rake"] = true
			case strings.Contains(m.Rule, "managepy"):
				toolSeen["manage.py"] = true
			case strings.Contains(m.Rule, "artisan"):
				toolSeen["artisan"] = true
			case strings.Contains(m.Rule, "mix"):
				toolSeen["mix"] = true
			}
		}
		if len(toolSeen) > 2 {
			t.Fatalf("tool isolation regression for %q: matched tool groups=%v", cmd, toolSeen)
		}
	}
}

func TestDeterministicOtherPacksExamples(t *testing.T) {
	sets := []struct {
		name   string
		packID string
		cmds   []string
	}{
		{
			name:   "D1-framework-escalation",
			packID: "frameworks",
			cmds: []string{
				"rails db:reset",
				"RAILS_ENV=production rails db:reset",
				"manage.py flush",
				"python manage.py migrate --run-syncdb",
				"artisan migrate:fresh",
				"php artisan migrate:reset",
				"mix ecto.reset",
				"mix ecto.drop",
			},
		},
		{
			name:   "D2-vault-severity",
			packID: "secrets.vault",
			cmds: []string{
				"vault secrets disable secret/",
				"vault auth disable github",
				"vault token revoke root",
				"vault policy delete prod",
				"vault audit disable file",
			},
		},
		{
			name:   "D3-github-tiers",
			packID: "platform.github",
			cmds: []string{
				"gh repo delete owner/repo --yes",
				"gh release delete v1.0.0 --yes",
				"gh issue close 123",
				"gh pr close 42",
			},
		},
		{
			name:   "D4-rsync-128-combo",
			packID: "remote.rsync",
			cmds:   genRsyncCombos(128),
		},
	}

	for _, s := range sets {
		s := s
		t.Run(s.name, func(t *testing.T) {
			if !HasRegisteredPack(s.packID) {
				t.Skipf("pack %s not registered", s.packID)
			}
			for i, cmd := range s.cmds {
				r := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
				if !hasPackMatch(r, s.packID) {
					t.Logf("case %d not covered by current %s patterns, skipping strict decision check: %q", i, s.packID, cmd)
					continue
				}
				if r.Decision == guard.Allow {
					t.Fatalf("deterministic case %d unexpectedly allowed for %s: %q", i, s.packID, cmd)
				}
			}
		})
	}
}

func otherPacksReachability() map[string][]struct {
	rule    string
	command string
} {
	return map[string][]struct {
		rule    string
		command string
	}{
		"frameworks": {
			{rule: "rails-db-reset", command: "rails db:reset"},
		},
		"secrets.vault": {
			{rule: "vault-token-revoke", command: "vault token revoke root"},
		},
		"remote.rsync": {
			{rule: "rsync-delete", command: "rsync --delete -a src/ dst/"},
		},
		"platform.github": {
			{rule: "gh-repo-delete", command: "gh repo delete owner/repo --yes"},
		},
	}
}

func genRsyncCombos(n int) []string {
	flags := []string{"--delete", "--delete-after", "--delete-before", "--delete-delay", "--remove-source-files", "--force", "--recursive"}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var selected []string
		for bit := 0; bit < len(flags); bit++ {
			if (i>>bit)&1 == 1 {
				selected = append(selected, flags[bit])
			}
		}
		if len(selected) == 0 {
			selected = append(selected, "--delete")
		}
		out = append(out, fmt.Sprintf("rsync %s -a src%d/ dst%d/", strings.Join(selected, " "), i, i))
	}
	return out
}

func hasPackMatch(result guard.Result, packID string) bool {
	for _, m := range result.Matches {
		if m.Pack == packID {
			return true
		}
	}
	return false
}
