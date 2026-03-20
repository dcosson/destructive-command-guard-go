package e2etest

import (
	"path/filepath"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func BenchmarkEvaluateFullPipeline(b *testing.B) {
	scenarios := []struct {
		name string
		cmd  string
		opts []guard.Option
	}{
		{"git_force", "git push --force origin main", []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{"rm_root", "rm -rf /", []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{"safe_echo", "echo hello", []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{"safe_git", "git status", []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{"env_inline", "RAILS_ENV=production rails db:reset", []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{"env_process", "rails db:reset", []guard.Option{guard.WithEnv([]string{"RAILS_ENV=production"})}},
		{"allowlisted", "git push --force", []guard.Option{guard.WithAllowlist("git push *")}},
		{"blocklisted", "echo safe", []guard.Option{guard.WithBlocklist("echo *")}},
		{"compound_mixed", "echo deploy && git push --force && rm -rf /tmp/build", nil},
		{"empty", "", nil},
	}

	for _, sc := range scenarios {
		sc := sc
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = guard.Evaluate(sc.cmd, sc.opts...)
			}
		})
	}
}

func TestBenchmarkBaselineSerialization(t *testing.T) {
	results := []BenchResult{
		{Name: "BenchmarkEvaluateFullPipeline/git_force", NsPerOp: 100, AllocsPerOp: 10, BytesPerOp: 320},
		{Name: "BenchmarkEvaluateFullPipeline/safe_echo", NsPerOp: 50, AllocsPerOp: 5, BytesPerOp: 128},
	}
	path := filepath.Join(t.TempDir(), "bench", "baseline.json")
	if err := WriteBenchResults(path, results); err != nil {
		t.Fatalf("WriteBenchResults error: %v", err)
	}
}
