package e2etest

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestGoldenValidationSchemaCoverageFreshness(t *testing.T) {
	path := filepath.Join("testdata", "golden", "expanded_corpus.tsv")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// Freshness guardrail: corpus should be updated within the last 2 years.
	if ageHours := timeSince(info.ModTime()); ageHours > 24*365*2 {
		t.Fatalf("golden corpus appears stale: age=%.0f hours", ageHours)
	}

	entries := loadExpandedGoldenEntries(t)
	if len(entries) == 0 {
		t.Fatal("expanded golden corpus is empty")
	}

	packCoverage := map[string]int{}
	for i, e := range entries {
		if strings.TrimSpace(e.Context) == "" || strings.TrimSpace(e.Command) == "" {
			t.Fatalf("invalid entry[%d]: empty context/command", i)
		}
		if e.Pack != "-" && strings.TrimSpace(e.Pack) == "" {
			t.Fatalf("invalid entry[%d]: empty pack", i)
		}
		if e.Rule != "-" && strings.TrimSpace(e.Rule) == "" {
			t.Fatalf("invalid entry[%d]: empty rule", i)
		}
		if e.Pack != "-" {
			packCoverage[e.Pack]++
		}
	}

	for _, p := range guard.Packs() {
		if packCoverage[p.ID] == 0 {
			t.Logf("pack %s currently has no expanded-corpus entries", p.ID)
		}
	}
}

func TestGoldenDecisionFileSelfConsistency(t *testing.T) {
	path := filepath.Join("..", "..", "guard", "testdata", "golden", "commands.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	byCommand := map[string]string{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		txt := strings.TrimSpace(sc.Text())
		if txt == "" || strings.HasPrefix(txt, "#") {
			continue
		}
		parts := strings.SplitN(txt, "|", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid decision golden line %d: %q", line, txt)
		}
		decision := strings.TrimSpace(parts[0])
		command := strings.TrimSpace(parts[1])
		if prev, ok := byCommand[command]; ok && prev != decision {
			t.Fatalf("contradictory golden decisions for %q: %s vs %s", command, prev, decision)
		}
		byCommand[command] = decision
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
}

func TestPropertyComparisonClassificationDeterministicExtended(t *testing.T) {
	known := map[string]KnownDivergence{
		"echo safe": {Command: "echo safe", Classification: "intentional_divergence"},
	}
	samples := []ComparisonEntry{
		{Command: "echo safe", GoDecision: "Allow", RustDecision: "Deny"},
		{Command: "git push --force", GoDecision: "Deny", RustDecision: "Allow"},
		{Command: "echo hello", GoDecision: "Allow", RustDecision: "Allow", GoSeverity: "Low", RustSeverity: "Low"},
		{Command: "rm -rf /", GoDecision: "Deny", RustDecision: "Deny", GoSeverity: "Critical", RustSeverity: "Low"},
	}
	for _, s := range samples {
		c1 := classifyDivergence(s, known)
		c2 := classifyDivergence(s, known)
		if c1 != c2 {
			t.Fatalf("classification non-deterministic for %q: %q vs %q", s.Command, c1, c2)
		}
	}
}

func TestComparisonInfrastructureParsers(t *testing.T) {
	cases := []struct {
		name string
		out  []byte
		want string
	}{
		{name: "json-deny", out: []byte(`{"decision":"deny","severity":"High","pack":"core.git"}`), want: "Deny"},
		{name: "text-allow", out: []byte("allow"), want: "Allow"},
		{name: "text-ask", out: []byte("ASK"), want: "Ask"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseUpstreamOutput(tc.out, nil, nil)
			if err != nil {
				t.Fatalf("parseUpstreamOutput error: %v", err)
			}
			if parsed.Decision != tc.want {
				t.Fatalf("decision=%s want=%s", parsed.Decision, tc.want)
			}
		})
	}
}

func TestBenchmarkStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stability test in short mode")
	}
	benchmarks := []struct {
		name string
		fn   func(b *testing.B)
	}{
		{
			name: "evaluate-safe",
			fn: func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = guard.Evaluate("echo hello")
				}
			},
		},
		{
			name: "evaluate-destructive",
			fn: func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					_ = guard.Evaluate("git push --force", guard.WithPolicy(guard.InteractivePolicy()))
				}
			},
		},
	}
	for _, bm := range benchmarks {
		bm := bm
		t.Run(bm.name, func(t *testing.T) {
			samples := make([]float64, 5)
			for i := range samples {
				r := testing.Benchmark(bm.fn)
				samples[i] = float64(r.NsPerOp())
			}
			mean, cv := meanAndCV(samples)
			maxCV := 0.30
			if mean > 100_000 {
				maxCV = 0.15
			}
			if cv > maxCV {
				t.Fatalf("benchmark %s too unstable: mean=%.0fns cv=%.2f max=%.2f", bm.name, mean, cv, maxCV)
			}
		})
	}
}

func TestBenchmarkRegressionDetectionThreshold(t *testing.T) {
	base := BenchResult{Name: "x", NsPerOp: 100}
	minor := BenchResult{Name: "x", NsPerOp: 110}
	major := BenchResult{Name: "x", NsPerOp: 130}
	improve := BenchResult{Name: "x", NsPerOp: 50}
	if isRegression(base, minor, 0.20) {
		t.Fatal("10% regression should not trip 20% threshold")
	}
	if !isRegression(base, major, 0.20) {
		t.Fatal("30% regression should trip 20% threshold")
	}
	if isRegression(base, improve, 0.20) {
		t.Fatal("improvement should not be marked regression")
	}
}

func timeSince(ts time.Time) float64 {
	return time.Since(ts).Hours()
}

func meanAndCV(samples []float64) (mean float64, cv float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	for _, x := range samples {
		mean += x
	}
	mean /= float64(len(samples))
	if mean == 0 {
		return mean, 0
	}
	var ss float64
	for _, x := range samples {
		d := x - mean
		ss += d * d
	}
	std := math.Sqrt(ss / float64(len(samples)))
	return mean, std / mean
}

func isRegression(base, current BenchResult, threshold float64) bool {
	if base.NsPerOp <= 0 {
		return false
	}
	return ((current.NsPerOp - base.NsPerOp) / base.NsPerOp) > threshold
}

func TestFaultComparisonNoUpstreamBinary(t *testing.T) {
	if os.Getenv("UPSTREAM_BINARY") != "" {
		t.Skip("UPSTREAM_BINARY set; skip missing-binary behavior check")
	}
	t.Log("comparison harness should skip when UPSTREAM_BINARY is unset")
}

func TestBenchmarkInfrastructureJSONRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bench", "results.json")
	in := []BenchResult{
		{Name: "A", NsPerOp: 100, AllocsPerOp: 1, BytesPerOp: 64},
		{Name: "B", NsPerOp: 200, AllocsPerOp: 2, BytesPerOp: 128},
	}
	if err := WriteBenchResults(path, in); err != nil {
		t.Fatalf("WriteBenchResults: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []BenchResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal bench json: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("results length mismatch: %d vs %d", len(out), len(in))
	}
}
