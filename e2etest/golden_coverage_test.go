package e2etest

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

type goldenCorpusEntry struct {
	Context string
	Pack    string
	Rule    string
	Command string
}

func TestGoldenCoverageAllPatterns(t *testing.T) {
	entries := loadExpandedGoldenEntries(t)
	if len(entries) < 750 {
		t.Fatalf("golden corpus too small: got %d, want >= 750", len(entries))
	}

	coverage := make(map[string]int)
	for _, e := range entries {
		if e.Pack == "-" || e.Rule == "-" || e.Pack == "" || e.Rule == "" {
			continue
		}
		result := guard.Evaluate(e.Command, guard.WithPolicy(guard.InteractivePolicy()))
		if hasMatch(result, e.Pack, e.Rule) {
			coverage[e.Pack+"."+e.Rule]++
		}
	}

	for _, p := range packs.DefaultRegistry.All() {
		for _, r := range p.Destructive {
			key := p.ID + "." + r.ID
			count := coverage[key]
			if count < 3 {
				t.Fatalf("pattern %s has only %d golden entries (need >=3)", key, count)
			}
		}
	}
}

func TestGoldenCoverageStructuralContexts(t *testing.T) {
	entries := loadExpandedGoldenEntries(t)
	contexts := map[string]int{
		"simple":   0,
		"compound": 0,
		"subshell": 0,
		"pipeline": 0,
		"variable": 0,
		"inline":   0,
	}

	for _, e := range entries {
		ctx := classifyStructuralContext(e.Command)
		if _, ok := contexts[ctx]; ok {
			contexts[ctx]++
		}
	}

	for ctx, count := range contexts {
		if count == 0 {
			t.Fatalf("no golden entries for structural context: %s", ctx)
		}
	}
}

func loadExpandedGoldenEntries(t *testing.T) []goldenCorpusEntry {
	t.Helper()
	path := filepath.Join("testdata", "golden", "expanded_corpus.tsv")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var entries []goldenCorpusEntry
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		txt := strings.TrimSpace(sc.Text())
		if txt == "" || strings.HasPrefix(txt, "#") {
			continue
		}
		parts := strings.SplitN(txt, "\t", 4)
		if len(parts) != 4 {
			t.Fatalf("invalid corpus line %d: %q", line, txt)
		}
		entries = append(entries, goldenCorpusEntry{
			Context: parts[0],
			Pack:    parts[1],
			Rule:    parts[2],
			Command: parts[3],
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan corpus: %v", err)
	}
	return entries
}

func hasMatch(result guard.Result, packID, rule string) bool {
	for _, m := range result.Matches {
		if m.Pack == packID && m.Rule == rule {
			return true
		}
	}
	return false
}

func classifyStructuralContext(command string) string {
	cmd := strings.TrimSpace(command)
	switch {
	case strings.Contains(cmd, "bash -c"):
		return "inline"
	case strings.Contains(cmd, "$DIR") || strings.Contains(cmd, "RAILS_ENV=") || strings.Contains(cmd, "DIR="):
		return "variable"
	case strings.HasPrefix(cmd, "("):
		return "subshell"
	case strings.Contains(cmd, "|"):
		return "pipeline"
	case strings.Contains(cmd, "&&") || strings.Contains(cmd, ";"):
		return "compound"
	default:
		return "simple"
	}
}
