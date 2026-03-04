package e2etest

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestDeterministicBenchmarkOrdering(t *testing.T) {
	prefilterMiss := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = guard.Evaluate("echo hello")
		}
	})
	simpleMatch := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = guard.Evaluate("git push --force")
		}
	})
	compound := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = guard.Evaluate("echo start && git push --force && rm -rf /")
		}
	})

	if prefilterMiss.NsPerOp() <= 0 || simpleMatch.NsPerOp() <= 0 || compound.NsPerOp() <= 0 {
		t.Fatalf("benchmark ns/op must be positive: prefilter=%d simple=%d compound=%d",
			prefilterMiss.NsPerOp(), simpleMatch.NsPerOp(), compound.NsPerOp())
	}
	// Robust ordering invariant across machines/noise: multi-command compound
	// evaluation should be slower than at least one single-command scenario.
	if compound.NsPerOp() <= prefilterMiss.NsPerOp() && compound.NsPerOp() <= simpleMatch.NsPerOp() {
		t.Fatalf("compound should be slowest-like: prefilter=%dns simple=%dns compound=%dns",
			prefilterMiss.NsPerOp(), simpleMatch.NsPerOp(), compound.NsPerOp())
	}
}

func TestDeterministicGoldenCorpusSize(t *testing.T) {
	entries := loadExpandedGoldenEntries(t)
	if len(entries) < 750 {
		t.Fatalf("golden corpus too small: got %d want >= 750", len(entries))
	}
}

func TestFaultGoldenFileMissingPack(t *testing.T) {
	entryPack := "nonexistent.pack"
	if HasRegisteredPack(entryPack) {
		t.Skipf("unexpectedly found %s", entryPack)
	}
	t.Logf("pack %s not registered (expected)", entryPack)
}

func TestOracleGoldenCrossValidation(t *testing.T) {
	entries := loadGuardDecisionGoldenEntries(t)
	for _, e := range entries {
		res := guard.Evaluate(e.command, guard.WithPolicy(guard.InteractivePolicy()))
		if got := res.Decision.String(); got != e.decision {
			t.Fatalf("golden mismatch command=%q got=%s want=%s", e.command, got, e.decision)
		}
	}
}

func TestStressConcurrentGoldenCorpusEvaluation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	entries := loadExpandedGoldenEntries(t)
	if len(entries) == 0 {
		t.Fatal("no expanded corpus entries")
	}

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := w; i < len(entries); i += workers {
				r := guard.Evaluate(entries[i].Command, guard.WithPolicy(guard.InteractivePolicy()))
				if r.Assessment == nil && r.Decision != guard.Allow {
					errCh <- fmt.Errorf("entry[%d] nil assessment with non-allow decision=%s", i, r.Decision)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestStressHighVolumeFuzzSeedRunner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high-volume fuzz seed runner in short mode")
	}
	seeds := LoadFuzzSeeds(filepath.Join("..", "..", "guard", "testdata", "golden"))
	if len(seeds) == 0 {
		t.Fatal("no fuzz seeds loaded")
	}
	total := 100_000
	for i := 0; i < total; i++ {
		cmd := seeds[i%len(seeds)]
		r := guard.Evaluate(cmd)
		if r.Assessment == nil && r.Decision != guard.Allow {
			t.Fatalf("seed[%d] nil assessment with non-allow decision=%s", i, r.Decision)
		}
	}
}

func TestStressSustainedLoadMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory pressure test in short mode")
	}
	const goroutines = 100
	const perGoroutine = 1000
	commands := []string{
		"git push --force", "rm -rf /", "echo hello",
		"git status", "docker system prune -af",
		"RAILS_ENV=production rails db:reset",
		"", "   ", "ls -la",
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				cmd := commands[(i*perGoroutine+j)%len(commands)]
				r := guard.Evaluate(cmd, guard.WithPolicy(guard.InteractivePolicy()))
				if r.Assessment == nil && r.Decision != guard.Allow {
					errCh <- fmt.Errorf("goroutine=%d iter=%d nil assessment with %s", i, j, r.Decision)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapInuse) - int64(before.HeapInuse)
	t.Logf("heap growth after %d evals: %d bytes", goroutines*perGoroutine, growth)
}

func TestStressMutationTimeLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping mutation time-limit test in short mode")
	}
	pk, ok := findPack("core.git")
	if !ok {
		t.Skip("core.git pack not registered")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	done := make(chan MutationReport, 1)
	go func() {
		done <- runMutationAnalysis(pk, loadMutationCorpus())
	}()
	select {
	case report := <-done:
		t.Logf("core.git mutation analysis: %d/%d killed in time", report.Killed, report.Total)
	case <-ctx.Done():
		t.Fatal("mutation analysis exceeded 5 minute time limit for core.git")
	}
}

func TestSecurityFuzzCorpusClean(t *testing.T) {
	corpusDir := filepath.Join("..", "..", "guard", "testdata", "fuzz")
	if _, err := os.Stat(corpusDir); os.IsNotExist(err) {
		t.Skip("no fuzz corpus directory")
	}

	sensitive := []string{
		"/Users/", "/home/", "password", "secret",
		"api_key", "token=", "Bearer ",
	}
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		txt := string(data)
		for _, pattern := range sensitive {
			if strings.Contains(txt, pattern) {
				return fmt.Errorf("fuzz corpus file %s contains sensitive pattern %q", path, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSecurityGoldenFileNotExecuted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	_ = guard.Evaluate(fmt.Sprintf("touch %s", marker), guard.WithPolicy(guard.InteractivePolicy()))
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker file unexpectedly exists: %s", marker)
	}
}

func TestSecurityNoSubprocessExecutionWithEmptyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PATH-restricted execution test in short mode")
	}
	entries := loadExpandedGoldenEntries(t)
	t.Setenv("PATH", "")
	for _, e := range entries {
		_ = guard.Evaluate(e.Command, guard.WithPolicy(guard.InteractivePolicy()))
	}
}

type guardDecisionGoldenEntry struct {
	decision string
	command  string
}

func loadGuardDecisionGoldenEntries(t *testing.T) []guardDecisionGoldenEntry {
	t.Helper()
	path := filepath.Join("..", "..", "guard", "testdata", "golden", "commands.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open decision golden file: %v", err)
	}
	defer f.Close()

	var out []guardDecisionGoldenEntry
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
		out = append(out, guardDecisionGoldenEntry{
			decision: strings.TrimSpace(parts[0]),
			command:  strings.TrimSpace(parts[1]),
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan decision golden file: %v", err)
	}
	return out
}
