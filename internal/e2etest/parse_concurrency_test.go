package e2etest

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// S1: Concurrent parsing stress with race-safety assertions.
func TestConcurrentParsingStress(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	startHeap := heapInuseBytes()

	const goroutines = 100
	iterationsPerGoroutine := 1000
	if testing.Short() {
		iterationsPerGoroutine = 200
	}

	var wg sync.WaitGroup
	var parsed uint64
	errs := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs <- fmt.Errorf("panic in goroutine %d: %v", g, r)
				}
			}()

			for i := 0; i < iterationsPerGoroutine; i++ {
				input := pickStressInput(g, i)
				result := parser.ParseAndExtract(context.Background(), input, 0)
				if err := validateStressResult(result, input); err != nil {
					errs <- fmt.Errorf("goroutine %d iter %d: %w", g, i, err)
					return
				}
				atomic.AddUint64(&parsed, 1)
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	endHeap := heapInuseBytes()
	// This is a coarse guardrail for catastrophic leaks during concurrent use.
	// Precise leak detection is covered by the dedicated S2 soak test.
	const maxGrowthBytes = 512 * 1024 * 1024
	if endHeap > startHeap+maxGrowthBytes {
		t.Fatalf("heap growth too high in concurrent stress: start=%d end=%d growth=%d", startHeap, endHeap, endHeap-startHeap)
	}

	t.Logf("concurrent stress parsed %d commands (%d goroutines x %d iterations)", parsed, goroutines, iterationsPerGoroutine)
}

func pickStressInput(goroutine, iteration int) string {
	if len(realWorldCommands) > 0 {
		idx := (goroutine*131 + iteration*17) % len(realWorldCommands)
		if realWorldCommands[idx] != "" {
			return realWorldCommands[idx]
		}
	}
	return generateRandomCommand(goroutine, iteration)
}

func validateStressResult(result ParseResult, input string) error {
	for _, w := range result.Warnings {
		if w.Message == "" {
			return fmt.Errorf("warning with empty message")
		}
	}

	for _, cmd := range result.Commands {
		if cmd.Name == "" {
			return fmt.Errorf("empty command name")
		}
		if cmd.StartByte > cmd.EndByte {
			return fmt.Errorf("invalid byte span: start=%d end=%d", cmd.StartByte, cmd.EndByte)
		}
	}

	return nil
}

func heapInuseBytes() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}
