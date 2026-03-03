package testharness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestComparisonAgainstUpstream(t *testing.T) {
	upstream := os.Getenv("UPSTREAM_BINARY")
	if upstream == "" {
		t.Skip("UPSTREAM_BINARY not set; skipping comparison tests")
	}

	corpusPath := filepath.Join("testdata", "comparison_corpus.json")
	divPath := filepath.Join("testdata", "comparison_divergences.json")
	reportPath := filepath.Join("testdata", "comparison_report.json")

	corpus, err := loadComparisonCorpus(corpusPath)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	known, err := loadKnownDivergences(divPath)
	if err != nil {
		t.Fatalf("load known divergences: %v", err)
	}

	results := make([]ComparisonEntry, 0, len(corpus))
	for _, entry := range corpus {
		goResult := guard.Evaluate(entry.Command, guard.WithPolicy(guard.InteractivePolicy()))
		rustResult, err := runUpstream(upstream, entry.Command)
		if err != nil {
			t.Fatalf("run upstream for %q: %v", entry.Command, err)
		}

		result := ComparisonEntry{
			Command:      entry.Command,
			GoDecision:   goResult.Decision.String(),
			GoSeverity:   severityString(goResult),
			RustDecision: rustResult.Decision,
			RustSeverity: rustResult.Severity,
			RustPack:     rustResult.Pack,
		}
		if len(goResult.Matches) > 0 {
			result.GoPack = goResult.Matches[0].Pack
		}

		if result.GoDecision == result.RustDecision {
			result.Classification = "identical"
		} else {
			result.Classification = classifyDivergence(result, known)
		}
		results = append(results, result)
	}

	if err := writeComparisonReport(reportPath, results); err != nil {
		t.Fatalf("write report: %v", err)
	}

	bugs := 0
	for _, r := range results {
		if r.Classification == "bug" {
			bugs++
		}
	}
	if bugs != 0 {
		t.Fatalf("found %d unexplained divergences classified as bug", bugs)
	}
}

func TestClassifyDivergenceDeterministic(t *testing.T) {
	known := map[string]KnownDivergence{
		"echo \"don't rm -rf /\"": {
			Command:        "echo \"don't rm -rf /\"",
			Classification: "intentional_improvement",
		},
	}

	samples := []ComparisonEntry{
		{
			Command:      "echo \"don't rm -rf /\"",
			GoDecision:   "Allow",
			RustDecision: "Deny",
		},
		{
			Command:      "git push --force",
			GoDecision:   "Deny",
			GoSeverity:   "High",
			RustDecision: "Allow",
			RustSeverity: "Low",
		},
		{
			Command:      "ls -la",
			GoDecision:   "Allow",
			RustDecision: "Deny",
		},
	}

	for _, s := range samples {
		c1 := classifyDivergence(s, known)
		c2 := classifyDivergence(s, known)
		if c1 != c2 {
			t.Fatalf("classification non-deterministic for %q: %q != %q", s.Command, c1, c2)
		}
	}
}
