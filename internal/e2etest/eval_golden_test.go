//go:build e2e

package e2etest

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// GoldenEntry represents a single golden file test case.
type GoldenEntry struct {
	Description  string
	Command      string
	HasCommand   bool // True if command: field was explicitly set
	Decision     string
	Severity     string // Empty if Allow
	Confidence   string // Empty if Allow
	Pack         string // Empty if Allow
	Rule         string // Empty if Allow
	EnvEscalated string // Empty if false or Allow
	Warnings     []string
	File         string // Source file for error reporting
	Line         int    // Line number for error reporting
}

// LoadCorpus loads all golden files from the corpus directory.
func LoadCorpus(tb testing.TB, dir string) []GoldenEntry {
	tb.Helper()
	var entries []GoldenEntry
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, ".txt") {
			return nil
		}
		fileEntries := parseGoldenFile(tb, path)
		entries = append(entries, fileEntries...)
		return nil
	})
	if err != nil {
		tb.Fatalf("walk corpus dir: %v", err)
	}
	return entries
}

func parseGoldenFile(tb testing.TB, path string) []GoldenEntry {
	tb.Helper()
	f, err := os.Open(path)
	if err != nil {
		tb.Fatalf("open golden file %s: %v", path, err)
	}
	defer f.Close()

	var entries []GoldenEntry
	scanner := bufio.NewScanner(f)
	lineNum := 0
	headerChecked := false
	var current *GoldenEntry
	entryStart := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if !headerChecked {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if trimmed != "format: v1" {
				tb.Fatalf("%s:%d: first non-comment line must be 'format: v1', got %q", path, lineNum, trimmed)
			}
			headerChecked = true
			continue
		}

		if strings.TrimSpace(line) == "---" {
			if current != nil {
				validateAndAdd(tb, path, current, &entries)
			}
			current = &GoldenEntry{File: path, Line: lineNum + 1}
			entryStart = lineNum
			continue
		}

		if current == nil {
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if current.Description == "" {
				current.Description = desc
				current.Line = entryStart + 1
			} else {
				current.Description += " " + desc
			}
			continue
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			tb.Errorf("%s:%d: invalid line (expected key: value): %q", path, lineNum, trimmed)
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "command":
			current.Command = val
			current.HasCommand = true
		case "decision":
			if val != "Allow" && val != "Deny" && val != "Ask" {
				tb.Errorf("%s:%d: invalid decision %q", path, lineNum, val)
			}
			current.Decision = val
		case "severity":
			valid := map[string]bool{"Critical": true, "High": true, "Medium": true, "Low": true, "Indeterminate": true}
			if !valid[val] {
				tb.Errorf("%s:%d: invalid severity %q", path, lineNum, val)
			}
			current.Severity = val
		case "confidence":
			valid := map[string]bool{"High": true, "Medium": true, "Low": true}
			if !valid[val] {
				tb.Errorf("%s:%d: invalid confidence %q", path, lineNum, val)
			}
			current.Confidence = val
		case "pack":
			current.Pack = val
		case "rule":
			current.Rule = val
		case "env_escalated":
			current.EnvEscalated = val
		case "warning":
			current.Warnings = append(current.Warnings, val)
		default:
			tb.Errorf("%s:%d: unknown key %q", path, lineNum, key)
		}
	}

	if current != nil {
		validateAndAdd(tb, path, current, &entries)
	}

	if err := scanner.Err(); err != nil {
		tb.Fatalf("reading %s: %v", path, err)
	}
	return entries
}

func validateAndAdd(tb testing.TB, path string, e *GoldenEntry, entries *[]GoldenEntry) {
	tb.Helper()
	if !e.HasCommand {
		tb.Errorf("%s:%d: entry missing 'command' field", e.File, e.Line)
		return
	}
	if e.Decision == "" {
		tb.Errorf("%s:%d: entry missing 'decision' field", e.File, e.Line)
		return
	}
	*entries = append(*entries, *e)
}

// RunCorpus runs all golden file entries against the pipeline.
func RunCorpus(t *testing.T, entries []GoldenEntry, pipeline *eval.Pipeline, cfg eval.Config) {
	t.Helper()
	for _, e := range entries {
		name := e.Description
		if name == "" {
			name = e.Command
		}
		t.Run(name, func(t *testing.T) {
			result := pipeline.Run(e.Command, cfg)

			wantDecision := parseGoldenDecision(e.Decision)
			if result.Decision != wantDecision {
				t.Errorf("%s:%d: command %q: decision = %v, want %s",
					e.File, e.Line, e.Command, result.Decision, e.Decision)
			}

			if e.Severity != "" && result.Assessment != nil {
				wantSev := parseGoldenSeverity(e.Severity)
				if result.Assessment.Severity != wantSev {
					t.Errorf("%s:%d: severity = %v, want %s",
						e.File, e.Line, result.Assessment.Severity, e.Severity)
				}
			}

			if e.Confidence != "" && result.Assessment != nil {
				wantConf := parseGoldenConfidence(e.Confidence)
				if result.Assessment.Confidence != wantConf {
					t.Errorf("%s:%d: confidence = %v, want %s",
						e.File, e.Line, result.Assessment.Confidence, e.Confidence)
				}
			}

			if e.Pack != "" {
				found := false
				for _, m := range result.Matches {
					if m.Pack == e.Pack {
						found = true
						break
					}
				}
				if !found {
					var gotPacks []string
					for _, m := range result.Matches {
						gotPacks = append(gotPacks, m.Pack)
					}
					t.Errorf("%s:%d: expected pack %q in matches, got %v",
						e.File, e.Line, e.Pack, gotPacks)
				}
			}

			if e.Rule != "" {
				found := false
				for _, m := range result.Matches {
					if m.Rule == e.Rule {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s:%d: expected rule %q in matches",
						e.File, e.Line, e.Rule)
				}
			}

			if e.EnvEscalated == "true" {
				anyEscalated := false
				for _, m := range result.Matches {
					if m.EnvEscalated {
						anyEscalated = true
						break
					}
				}
				if !anyEscalated {
					t.Errorf("%s:%d: expected env_escalated=true",
						e.File, e.Line)
				}
			}
		})
	}
}

func parseGoldenDecision(s string) eval.Decision {
	switch s {
	case "Allow":
		return eval.DecisionAllow
	case "Deny":
		return eval.DecisionDeny
	case "Ask":
		return eval.DecisionAsk
	}
	return -1
}

func parseGoldenSeverity(s string) eval.Severity {
	switch s {
	case "Critical":
		return eval.SeverityCritical
	case "High":
		return eval.SeverityHigh
	case "Medium":
		return eval.SeverityMedium
	case "Low":
		return eval.SeverityLow
	case "Indeterminate":
		return eval.SeverityIndeterminate
	}
	return -1
}

func parseGoldenConfidence(s string) eval.Confidence {
	switch s {
	case "High":
		return eval.ConfidenceHigh
	case "Medium":
		return eval.ConfidenceMedium
	case "Low":
		return eval.ConfidenceLow
	}
	return -1
}

// TestGoldenCorpus runs all golden file entries against the pipeline.
func TestGoldenCorpus(t *testing.T) {
	t.Parallel()
	pipeline := eval.NewPipeline(packs.DefaultRegistry)
	cfg := eval.Config{Policy: evalcore.InteractivePolicy()}

	dir := filepath.Join("..", "eval", "testdata", "golden")
	entries := LoadCorpus(t, dir)
	if len(entries) == 0 {
		t.Skip("no golden file entries found")
	}
	t.Logf("loaded %d golden entries", len(entries))
	RunCorpus(t, entries, pipeline, cfg)
}
