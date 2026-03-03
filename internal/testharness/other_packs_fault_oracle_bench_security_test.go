package testharness

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// F1-F2: Fault injection
func TestFaultOtherPacksMalformedCommands(t *testing.T) {
	longArg := strings.Repeat("a", 12_000)
	malformed := []string{
		"",
		" ",
		"\t\n",
		"rails",
		"vault",
		"gh",
		"rsync",
		"rails \"\"",
		"vault \"\"",
		"gh \"\"",
		"rails " + longArg,
		"vault " + longArg,
	}
	for i, cmd := range malformed {
		t.Run(fmt.Sprintf("malformed-%d", i), func(t *testing.T) {
			_ = guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		})
	}
}

func TestFaultOtherPacksUnicodeArguments(t *testing.T) {
	unicodeCmds := []string{
		"rails db:drôp",
		"vault deleté secret/production/中文",
		"gh repo delete org/répo-名前",
		"rsync --deléte /src/ /dest/",
	}
	for i, cmd := range unicodeCmds {
		t.Run(fmt.Sprintf("unicode-%d", i), func(t *testing.T) {
			res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
			for _, m := range res.Matches {
				switch m.Pack {
				case "frameworks", "secrets.vault", "remote.rsync", "platform.github":
					t.Fatalf("unicode variant unexpectedly matched %s/%s for command %q", m.Pack, m.Rule, cmd)
				}
			}
		})
	}
}

// O1-O2: Oracles / consistency
func TestOracleOtherPacksPolicyMonotonicity(t *testing.T) {
	commands := []string{
		"rails db:reset",
		"vault token revoke root",
		"rsync --delete -a src/ dst/",
		"gh repo delete owner/repo --yes",
	}
	restrict := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}
	for _, c := range commands {
		strict := guard.Evaluate(c, guard.WithPolicy(guard.StrictPolicy()))
		inter := guard.Evaluate(c, guard.WithPolicy(guard.InteractivePolicy()))
		perm := guard.Evaluate(c, guard.WithPolicy(guard.PermissivePolicy()))
		sr, ir, pr := restrict[strict.Decision], restrict[inter.Decision], restrict[perm.Decision]
		if sr < ir || ir < pr {
			t.Fatalf("policy monotonicity violated for %q: strict=%s inter=%s perm=%s", c, strict.Decision, inter.Decision, perm.Decision)
		}
	}
}

func TestOracleOtherPacksCrossPlanSeverityConsistency(t *testing.T) {
	type exp struct {
		packID   string
		ruleID   string
		severity guard.Severity
	}
	checks := []exp{
		{packID: "frameworks", ruleID: "rails-db-drop", severity: guard.High},
		{packID: "frameworks", ruleID: "rails-db-reset", severity: guard.High},
		{packID: "frameworks", ruleID: "rake-db-drop-all", severity: guard.Critical},
		{packID: "frameworks", ruleID: "managepy-flush", severity: guard.High},
		{packID: "frameworks", ruleID: "managepy-migrate-syncdb", severity: guard.Medium},
		{packID: "frameworks", ruleID: "artisan-migrate-fresh", severity: guard.High},
		{packID: "frameworks", ruleID: "mix-ecto-reset", severity: guard.High},
		{packID: "secrets.vault", ruleID: "vault-secrets-disable", severity: guard.Critical},
		{packID: "secrets.vault", ruleID: "vault-token-revoke", severity: guard.High},
		{packID: "platform.github", ruleID: "gh-repo-delete", severity: guard.Critical},
		{packID: "platform.github", ruleID: "gh-release-delete", severity: guard.High},
		{packID: "platform.github", ruleID: "gh-issue-pr-close", severity: guard.Low},
	}
	validated := 0
	for _, c := range checks {
		pk, ok := findPack(c.packID)
		if !ok {
			continue
		}
		r, ok := findRule(pk, c.ruleID)
		if !ok {
			continue
		}
		validated++
		if guard.Severity(r.Severity) != c.severity {
			t.Fatalf("severity mismatch for %s/%s: got=%d want=%s", c.packID, c.ruleID, r.Severity, c.severity)
		}
	}
	if validated == 0 {
		t.Skip("no target other-pack rules registered in current registry")
	}
}

// S1-S2: Stress
func TestStressHighVolumeOtherPacks(t *testing.T) {
	if testing.Short() {
		t.Skip("skip stress test in short mode")
	}
	commands := generateOtherPackCommandStream(100_000)
	results := make([]guard.Result, len(commands))
	for i, cmd := range commands {
		results[i] = guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
	}

	for i := 0; i < 1000; i++ {
		idx := i * 100
		r2 := guard.Evaluate(commands[idx], guard.WithPolicy(guard.InteractivePolicy()))
		if results[idx].Decision != r2.Decision {
			t.Fatalf("non-deterministic decision at index=%d cmd=%q first=%s second=%s", idx, commands[idx], results[idx].Decision, r2.Decision)
		}
	}
}

func TestStressConcurrentOtherPacks(t *testing.T) {
	if testing.Short() {
		t.Skip("skip stress test in short mode")
	}
	commands := generateOtherPackCommandStream(20_000)
	workers := 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := worker; j < len(commands); j += workers {
				_ = guard.Evaluate(commands[j], guard.WithPolicy(guard.InteractivePolicy()))
			}
		}(i)
	}
	wg.Wait()
}

// SEC1-SEC3: Security
func TestSecurityNoSecretLeakage(t *testing.T) {
	if !HasRegisteredPack("secrets.vault") {
		t.Skip("secrets.vault pack not registered")
	}
	sensitiveCommands := []string{
		"vault delete secret/production/api-keys/stripe",
		"vault kv destroy secret/prod/database/credentials",
		"vault lease revoke aws/creds/production-admin/abc123",
	}
	sensitiveFragments := []string{"stripe", "credentials", "production-admin", "api-keys"}
	for _, cmd := range sensitiveCommands {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		for _, m := range res.Matches {
			if m.Pack != "secrets.vault" {
				continue
			}
			reason := strings.ToLower(m.Reason)
			rem := strings.ToLower(m.Remediation)
			for _, frag := range sensitiveFragments {
				f := strings.ToLower(frag)
				if strings.Contains(reason, f) || strings.Contains(rem, f) {
					t.Fatalf("sensitive token %q leaked in reason/remediation for %q", frag, cmd)
				}
			}
		}
	}
}

func TestSecurityVaultS2NotClauses(t *testing.T) {
	if !HasRegisteredPack("secrets.vault") {
		t.Skip("secrets.vault pack not registered")
	}
	// destructive operations that safe inspect patterns should not whitelist
	destructive := []string{
		"vault auth disable userpass/",
		"vault token revoke s.abc123",
		"vault policy delete my-policy",
		"vault audit disable file/",
	}
	for _, cmd := range destructive {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if res.Decision == guard.Allow {
			t.Fatalf("vault destructive operation unexpectedly allowed: %q", cmd)
		}
	}

	safe := []string{
		"vault auth list",
		"vault auth enable userpass",
		"vault token lookup",
		"vault token create",
		"vault policy read my-policy",
		"vault policy list",
		"vault audit list",
	}
	for _, cmd := range safe {
		res := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		if res.Decision != guard.Allow {
			t.Fatalf("vault safe inspect operation unexpectedly blocked: %q => %s", cmd, res.Decision)
		}
	}
}

func TestSecurityFrameworkEnvEscalation(t *testing.T) {
	if !HasRegisteredPack("frameworks") {
		t.Skip("frameworks pack not registered")
	}
	base := guard.Evaluate("rails db:reset", guard.WithPolicy(guard.InteractivePolicy()))
	prod := guard.Evaluate("RAILS_ENV=production rails db:reset", guard.WithPolicy(guard.InteractivePolicy()))
	if base.Assessment == nil || prod.Assessment == nil {
		t.Skip("frameworks command not assessed in current registry")
	}
	if prod.Assessment.Severity < base.Assessment.Severity {
		t.Fatalf("env escalation regressed severity: base=%s prod=%s", base.Assessment.Severity, prod.Assessment.Severity)
	}
}

func TestSecurityNoUnexpectedHeapGrowthInOtherPacksBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skip heap growth check in short mode")
	}
	run := func(n int) uint64 {
		commands := generateOtherPackCommandStream(n)
		for _, cmd := range commands {
			_ = guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
		}
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}
	before := run(1000)
	after := run(10000)
	if after > before*3 && after-before > 64*1024*1024 {
		t.Fatalf("heap growth too high: before=%d after=%d", before, after)
	}
}

// B1-B2: Benchmarks
func BenchmarkOtherPacksFrameworksMatching(b *testing.B) {
	commands := map[string]string{
		"rails-reset":      "rails db:reset",
		"rails-safe":       "rails routes",
		"django-flush":     "python manage.py flush",
		"artisan-fresh":    "php artisan migrate:fresh",
		"mix-reset":        "mix ecto.reset",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkOtherPacksVaultMatching(b *testing.B) {
	commands := map[string]string{
		"secrets-disable": "vault secrets disable secret/",
		"token-revoke":    "vault token revoke root",
		"safe-status":     "vault status",
		"safe-policy-read": "vault policy read my-policy",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkOtherPacksRsyncMatching(b *testing.B) {
	commands := map[string]string{
		"delete":         "rsync --delete -a src/ dst/",
		"delete-before":  "rsync --delete-before -a src/ dst/",
		"remove-source":  "rsync --remove-source-files -a src/ dst/",
		"safe-copy":      "rsync -a src/ dst/",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkOtherPacksGitHubMatching(b *testing.B) {
	commands := map[string]string{
		"repo-delete":    "gh repo delete owner/repo --yes",
		"release-delete": "gh release delete v1.0.0 --yes",
		"issue-close":    "gh issue close 42",
		"safe-list":      "gh issue list",
	}
	benchGuardEvalCommands(b, commands)
}

func BenchmarkOtherPacksGoldenCorpusThroughput(b *testing.B) {
	var corpus []string
	for _, pairs := range otherPacksReachability() {
		for _, p := range pairs {
			corpus = append(corpus, p.command)
		}
	}
	if len(corpus) == 0 {
		b.Skip("no other-packs reachability corpus in current registry")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range corpus {
			_ = guard.Evaluate(c, guard.WithPolicy(guard.InteractivePolicy()))
		}
	}
}

func BenchmarkOtherPacksFrameworksFullPackEvalNoMatchWorstCase(b *testing.B) {
	cmd := "rails generate model User name:string"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
	}
}

func generateOtherPackCommandStream(n int) []string {
	base := []string{
		"rails db:reset",
		"rails routes",
		"vault token revoke root",
		"vault policy read my-policy",
		"rsync --delete -a src/ dst/",
		"rsync -a src/ dst/",
		"gh repo delete owner/repo --yes",
		"gh issue list",
	}
	out := make([]string, n)
	for i := range out {
		out[i] = base[i%len(base)]
	}
	return out
}
