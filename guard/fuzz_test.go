package guard_test

import (
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
	"github.com/dcosson/destructive-command-guard-go/internal/testharness"
)

func FuzzEvaluate(f *testing.F) {
	for _, seed := range testharness.LoadFuzzSeeds("testdata/golden") {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, command string) {
		result := guard.Evaluate(command)
		verifyInvariants(t, command, result)
	})
}

func FuzzEvaluateWithAllowlist(f *testing.F) {
	for _, seed := range testharness.LoadFuzzSeeds("testdata/golden") {
		f.Add(seed, "git *", "rm *")
	}
	f.Fuzz(func(t *testing.T, command, allowPattern, blockPattern string) {
		result := guard.Evaluate(
			command,
			guard.WithAllowlist(allowPattern),
			guard.WithBlocklist(blockPattern),
		)
		verifyInvariants(t, command, result)

		// INV-A1: exact allowlist match with no separators should allow.
		if patternMatchesWithoutSeparators(command, allowPattern) &&
			!containsSeparators(command) &&
			!patternMatchesWithoutSeparators(command, blockPattern) &&
			result.Decision != guard.Allow {
			t.Fatalf("INV-A1: command %q matches allowlist %q but decision %s", command, allowPattern, result.Decision)
		}

		// INV-A2: allowlist should not bypass after separators.
		if containsSeparators(command) && prefixMatchesPattern(command, allowPattern) &&
			!patternMatchesWithoutSeparators(command, blockPattern) {
			if result.Decision == guard.Allow {
				t.Fatalf("INV-A2: allowlist %q matched prefix in %q", allowPattern, command)
			}
		}

		// INV-B1: blocklist match always denies.
		if patternMatchesWithoutSeparators(command, blockPattern) && result.Decision != guard.Deny {
			t.Fatalf("INV-B1: command %q matches blocklist %q but got %s", command, blockPattern, result.Decision)
		}
	})
}

func verifyInvariants(t *testing.T, command string, result guard.Result) {
	t.Helper()

	// INV-1: decision must be valid enum.
	switch result.Decision {
	case guard.Allow, guard.Deny, guard.Ask:
	default:
		t.Fatalf("INV-1: invalid decision %d for %q", result.Decision, command)
	}

	// INV-2: command preserved.
	if result.Command != command {
		t.Fatalf("INV-2: command mismatch got %q want %q", result.Command, command)
	}

	// INV-3: empty/whitespace command allows.
	if strings.TrimSpace(command) == "" && result.Decision != guard.Allow {
		t.Fatalf("INV-3: whitespace command got %s want Allow", result.Decision)
	}

	// INV-4: nil assessment implies allow and no matches.
	if result.Assessment == nil {
		if result.Decision != guard.Allow {
			t.Fatalf("INV-4: nil assessment with %s decision", result.Decision)
		}
		if len(result.Matches) > 0 {
			t.Fatalf("INV-4: nil assessment with %d matches", len(result.Matches))
		}
	}

	// INV-5: matches implies assessment.
	if len(result.Matches) > 0 && result.Assessment == nil {
		t.Fatalf("INV-5: %d matches but nil assessment", len(result.Matches))
	}

	// INV-6: assessment severity valid.
	if result.Assessment != nil {
		switch result.Assessment.Severity {
		case guard.Indeterminate, guard.Low, guard.Medium, guard.High, guard.Critical:
		default:
			t.Fatalf("INV-6: invalid severity %d", result.Assessment.Severity)
		}
	}

	// INV-7: match fields populated.
	for i, m := range result.Matches {
		if m.Pack == "" {
			t.Fatalf("INV-7: match %d empty pack", i)
		}
		if m.Rule == "" && m.Pack != "_blocklist" && m.Pack != "_allowlist" {
			t.Fatalf("INV-7: match %d empty rule", i)
		}
	}

	// INV-8: oversized input should not escalate to high confidence severity.
	if len(command) > parse.MaxInputSize && result.Assessment != nil && result.Assessment.Severity > guard.Indeterminate {
		// This check is intentionally weak because current pipeline does not expose MaxCommandBytes separately.
		t.Fatalf("INV-8: oversized input (%d) got severity %s", len(command), result.Assessment.Severity)
	}
}

func patternMatchesWithoutSeparators(command, pattern string) bool {
	if pattern == "" {
		return false
	}
	if containsSeparators(command) {
		return false
	}
	// simple '*' matcher equivalent to current pipeline behavior
	c := strings.TrimSpace(command)
	p := strings.TrimSpace(pattern)
	if p == "*" {
		return !containsSeparators(c)
	}
	if !strings.Contains(p, "*") {
		return c == p
	}
	parts := strings.Split(p, "*")
	idx := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		pos := strings.Index(c[idx:], part)
		if pos < 0 {
			return false
		}
		idx += pos + len(part)
	}
	return true
}

func prefixMatchesPattern(command, pattern string) bool {
	if pattern == "" {
		return false
	}
	for _, sep := range []string{"&&", "||", ";", "|", "&"} {
		if i := strings.Index(command, sep); i >= 0 {
			return patternMatchesWithoutSeparators(strings.TrimSpace(command[:i]), pattern)
		}
	}
	return false
}

func containsSeparators(command string) bool {
	return strings.Contains(command, ";") ||
		strings.Contains(command, "|") ||
		strings.Contains(command, "&")
}
