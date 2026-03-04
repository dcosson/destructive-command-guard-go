package e2etest

import (
	"runtime"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestAllocationsEvaluate(t *testing.T) {
	// Warm pipeline singleton.
	_ = guard.Evaluate("echo hello")

	commands := []struct {
		name      string
		cmd       string
		maxAllocs float64
	}{
		{"prefilter_miss", "echo hello", 80},
		{"simple_match", "git push --force", 260},
		{"compound", "echo start && git push --force && rm -rf /tmp/e2e", 800},
		{"empty", "", 10},
	}

	for _, tc := range commands {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				_ = guard.Evaluate(tc.cmd, guard.WithPolicy(guard.InteractivePolicy()))
			})
			t.Logf("%s: %.0f allocs/op", tc.name, allocs)
			if allocs > tc.maxAllocs {
				t.Fatalf("allocs %.0f > max %.0f for %q", allocs, tc.maxAllocs, tc.cmd)
			}
		})
	}
}

func TestNoMemoryLeakUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory leak test in short mode")
	}

	run := func(n int) uint64 {
		for i := 0; i < n; i++ {
			_ = guard.Evaluate("git push --force && rm -rf /tmp/e2e", guard.WithPolicy(guard.InteractivePolicy()))
		}
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}

	before := run(1000)
	after := run(10000)
	// Allow headroom for runtime noise, but prevent unbounded growth.
	if after > before*3 && after-before > 64*1024*1024 {
		t.Fatalf("potential heap growth leak: before=%d after=%d", before, after)
	}
	t.Logf("heap alloc before=%d after=%d", before, after)
}
