package e2etest

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"testing"
)

// S2: Memory soak test. Intended for nightly or explicit runs.
func TestMemorySoakS2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}
	if os.Getenv("DCG_RUN_SOAK") != "1" {
		t.Skip("set DCG_RUN_SOAK=1 to run memory soak")
	}

	iterations := 1_000_000
	if v := os.Getenv("DCG_SOAK_ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			t.Fatalf("invalid DCG_SOAK_ITERATIONS=%q", v)
		}
		iterations = n
	}

	inputs := []string{
		"git push --force origin main",
		"DIR=/tmp || DIR=/; rm -rf $DIR",
		"cat /var/log/syslog | grep error | sort | uniq -c",
		`python -c "import os; os.system('rm -rf /tmp/x')"`,
		"bash <<'EOF'\nrm -rf /tmp/data\nEOF",
	}

	parser := NewBashParser()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	startHeap := m.HeapInuse
	maxHeap := startHeap

	ctx := context.Background()
	for i := 0; i < iterations; i++ {
		input := inputs[i%len(inputs)]
		result := parser.ParseAndExtract(ctx, input, 0)
		if len(result.Commands) == 0 {
			t.Fatalf("no commands extracted at iteration %d for input %q", i, input)
		}
		if i%100_000 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&m)
			if m.HeapInuse > maxHeap {
				maxHeap = m.HeapInuse
			}
			t.Logf("soak progress %d/%d: HeapInuse=%dMB", i, iterations, m.HeapInuse/1024/1024)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m)
	endHeap := m.HeapInuse
	if endHeap > maxHeap {
		maxHeap = endHeap
	}

	growth := endHeap - startHeap
	const maxGrowthBytes = 50 * 1024 * 1024
	if growth > maxGrowthBytes {
		t.Fatalf("possible memory leak: HeapInuse grew by %dMB (start=%dMB end=%dMB peak=%dMB)",
			growth/1024/1024, startHeap/1024/1024, endHeap/1024/1024, maxHeap/1024/1024)
	}

	t.Logf("memory soak complete: iterations=%d start=%dMB end=%dMB peak=%dMB growth=%dMB",
		iterations, startHeap/1024/1024, endHeap/1024/1024, maxHeap/1024/1024, growth/1024/1024)
}
