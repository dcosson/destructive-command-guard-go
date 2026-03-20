package integration

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

func FuzzEvaluate(f *testing.F) {
	root, err := FindModuleRoot()
	if err != nil {
		f.Fatalf("find module root: %v", err)
	}
	for _, seed := range LoadFuzzSeeds(filepath.Join(root, "guard", "testdata", "golden")) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, command string) {
		result := guard.Evaluate(command)
		guardVerifyInvariants(t, command, result)
	})
}

func FuzzEvaluateWithAllowlist(f *testing.F) {
	root, err := FindModuleRoot()
	if err != nil {
		f.Fatalf("find module root: %v", err)
	}
	for _, seed := range LoadFuzzSeeds(filepath.Join(root, "guard", "testdata", "golden")) {
		f.Add(seed, "git *", "rm *")
	}
	f.Fuzz(func(t *testing.T, command, allowPattern, blockPattern string) {
		result := guard.Evaluate(
			command,
			guard.WithAllowlist(allowPattern),
			guard.WithBlocklist(blockPattern),
		)
		guardVerifyInvariants(t, command, result)

		// INV-A1: exact allowlist match with no separators should allow.
		if guardGlobMatchProduction(allowPattern, command) &&
			guardContainsSeparators(command) == false &&
			!guardGlobMatchProduction(blockPattern, command) &&
			result.Decision != guard.Allow {
			t.Fatalf("INV-A1: command %q matches allowlist %q but decision %s", command, allowPattern, result.Decision)
		}

		// INV-A2: allowlist should not bypass after separators.
		if guardContainsSeparators(command) && guardPrefixMatchesPattern(command, allowPattern) &&
			!guardGlobMatchProduction(blockPattern, command) {
			baseline := guard.Evaluate(command, guard.WithBlocklist(blockPattern))
			if baseline.Decision != guard.Allow && result.Decision == guard.Allow {
				t.Fatalf("INV-A2: allowlist %q matched prefix in %q", allowPattern, command)
			}
		}

		// INV-B1: blocklist match always denies.
		if strings.TrimSpace(command) != "" &&
			guardGlobMatchProduction(blockPattern, command) &&
			result.Decision != guard.Deny {
			t.Fatalf("INV-B1: command %q matches blocklist %q but got %s", command, blockPattern, result.Decision)
		}
	})
}

func guardVerifyInvariants(t *testing.T, command string, result guard.Result) {
	t.Helper()

	switch result.Decision {
	case guard.Allow, guard.Deny, guard.Ask:
	default:
		t.Fatalf("INV-1: invalid decision %d for %q", result.Decision, command)
	}

	if result.Command != command {
		t.Fatalf("INV-2: command mismatch got %q want %q", result.Command, command)
	}

	if strings.TrimSpace(command) == "" && result.Decision != guard.Allow {
		t.Fatalf("INV-3: whitespace command got %s want Allow", result.Decision)
	}

	if !hasAnyAssessment(result) {
		if result.Decision != guard.Allow {
			t.Fatalf("INV-4: nil assessments with %s decision", result.Decision)
		}
		if len(result.Matches) > 0 {
			t.Fatalf("INV-4: nil assessments with %d matches", len(result.Matches))
		}
	}

	if len(result.Matches) > 0 && !hasAnyAssessment(result) {
		t.Fatalf("INV-5: %d matches but nil assessments", len(result.Matches))
	}

	if result.DestructiveAssessment != nil {
		switch result.DestructiveAssessment.Severity {
		case guard.Indeterminate, guard.Low, guard.Medium, guard.High, guard.Critical:
		default:
			t.Fatalf("INV-6: invalid severity %d", result.DestructiveAssessment.Severity)
		}
	}
	if result.PrivacyAssessment != nil {
		switch result.PrivacyAssessment.Severity {
		case guard.Indeterminate, guard.Low, guard.Medium, guard.High, guard.Critical:
		default:
			t.Fatalf("INV-6: invalid privacy severity %d", result.PrivacyAssessment.Severity)
		}
	}

	for i, m := range result.Matches {
		if m.Pack == "" {
			t.Fatalf("INV-7: match %d empty pack", i)
		}
		if m.Rule == "" && m.Pack != "_blocklist" && m.Pack != "_allowlist" {
			t.Fatalf("INV-7: match %d empty rule", i)
		}
	}

	if len(command) > parse.MaxInputSize && result.DestructiveAssessment != nil && result.DestructiveAssessment.Severity > guard.Indeterminate {
		t.Fatalf("INV-8: oversized input (%d) got severity %s", len(command), result.DestructiveAssessment.Severity)
	}
}

func guardPrefixMatchesPattern(command, pattern string) bool {
	if pattern == "" {
		return false
	}
	for _, sep := range []string{"&&", "||", ";", "|", "&"} {
		if i := strings.Index(command, sep); i >= 0 {
			return guardGlobMatchProduction(pattern, strings.TrimSpace(command[:i]))
		}
	}
	return false
}

func guardContainsSeparators(command string) bool {
	return strings.Contains(command, ";") ||
		strings.Contains(command, "|") ||
		strings.Contains(command, "&")
}

func guardGlobMatchProduction(pattern, command string) bool {
	p := strings.TrimSpace(pattern)
	c := strings.TrimSpace(command)
	if p == "" {
		return false
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range p {
		switch r {
		case '*':
			b.WriteString(`[^;|&]*`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(c)
}
