package e2etest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/eval"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func TestOracleInternalVsPublicEquivalence(t *testing.T) {
	pipeline := eval.NewPipeline(packs.DefaultRegistry)
	commands := []string{
		"git push --force origin main",
		"rm -rf /",
		"echo hello",
		"git status",
		"RAILS_ENV=production rails db:reset",
	}

	for _, cmd := range commands {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			policy := guard.InteractivePolicy()
			public := guard.Evaluate(cmd, guard.WithDestructivePolicy(policy))
			internal := pipeline.Run(cmd, eval.Config{DestructivePolicy: policy, PrivacyPolicy: policy})

			if public.Decision != internal.Decision {
				t.Fatalf("decision mismatch command=%q public=%s internal=%s", cmd, public.Decision, internal.Decision)
			}
			if len(public.Matches) != len(internal.Matches) {
				t.Fatalf("matches len mismatch command=%q public=%d internal=%d", cmd, len(public.Matches), len(internal.Matches))
			}
			if (public.DestructiveAssessment == nil) != (internal.DestructiveAssessment == nil) {
				t.Fatalf("assessment nil mismatch command=%q", cmd)
			}
			if public.DestructiveAssessment != nil && internal.DestructiveAssessment != nil {
				if public.DestructiveAssessment.Severity != internal.DestructiveAssessment.Severity {
					t.Fatalf("assessment severity mismatch command=%q public=%s internal=%s", cmd, public.DestructiveAssessment.Severity, internal.DestructiveAssessment.Severity)
				}
			}
		})
	}
}

func TestOracleRustComparisonIfAvailable(t *testing.T) {
	upstream := os.Getenv("UPSTREAM_BINARY")
	if upstream == "" {
		t.Skip("UPSTREAM_BINARY not set")
	}
	commands := []string{
		"echo hello",
		"git status",
		"git push --force",
		"rm -rf /",
	}
	for _, cmd := range commands {
		cmd := cmd
		t.Run(cmd, func(t *testing.T) {
			goResult := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			rustDecision, err := guardRunUpstreamDecision(upstream, cmd)
			if err != nil {
				t.Fatalf("run upstream: %v", err)
			}
			if rustDecision == "" {
				t.Fatalf("empty rust decision for %q", cmd)
			}
			if goResult.Decision.String() != rustDecision {
				t.Logf("divergence command=%q go=%s rust=%s", cmd, goResult.Decision, rustDecision)
			}
		})
	}
}

func guardRunUpstreamDecision(binary, command string) (string, error) {
	c := exec.Command(binary, "check", command)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil && out == "" {
		return "", fmt.Errorf("upstream run failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if out == "" {
		return "", fmt.Errorf("empty upstream output")
	}
	var obj struct {
		Decision string `json:"decision"`
	}
	if json.Unmarshal(stdout.Bytes(), &obj) == nil && obj.Decision != "" {
		return guardNormalizeDecision(obj.Decision), nil
	}
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "deny"):
		return "Deny", nil
	case strings.Contains(low, "ask"):
		return "Ask", nil
	case strings.Contains(low, "allow"):
		return "Allow", nil
	default:
		return "", fmt.Errorf("unrecognized upstream output: %s", out)
	}
}

func guardNormalizeDecision(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "deny":
		return "Deny"
	case "ask":
		return "Ask"
	default:
		return "Allow"
	}
}

func BenchmarkEvaluateThroughputMatrix(b *testing.B) {
	cases := []struct {
		name string
		cmd  string
		opts []guard.Option
	}{
		{name: "destructive", cmd: "git push --force", opts: []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{name: "safe", cmd: "echo hello", opts: []guard.Option{guard.WithDestructivePolicy(guard.InteractivePolicy())}},
		{name: "allowlist", cmd: "git push --force", opts: []guard.Option{guard.WithAllowlist("git push *")}},
		{name: "blocklist", cmd: "echo hello", opts: []guard.Option{guard.WithBlocklist("echo *")}},
		{name: "empty", cmd: "", opts: nil},
	}
	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = guard.Evaluate(tc.cmd, tc.opts...)
			}
		})
	}
}

func BenchmarkEvaluateOptionOverhead(b *testing.B) {
	baseCmd := "echo hello"
	b.Run("baseline", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = guard.Evaluate(baseCmd)
		}
	})
	b.Run("with-options", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = guard.Evaluate(baseCmd,
				guard.WithDestructivePolicy(guard.StrictPolicy()),
				guard.WithAllowlist("git *"),
				guard.WithBlocklist("rm -rf *"),
				guard.WithDisabledPacks("platform.github"),
				guard.WithEnv([]string{"RAILS_ENV=production"}),
			)
		}
	})
}

func TestStressConcurrentEvaluate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	const goroutines = 100
	const iterations = 1000
	commands := []string{"git push --force", "rm -rf /", "echo hello", "git status", "RAILS_ENV=production rails db:reset", ""}

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cmd := commands[(i+j)%len(commands)]
				result := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
				if result.DestructiveAssessment == nil && result.Decision != guard.Allow {
					errCh <- fmt.Errorf("nil assessment with non-allow decision: %s", result.Decision)
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

func TestStressRapidInitializationSubprocess(t *testing.T) {
	if os.Getenv("DCG_RAPID_INIT_CHILD") == "1" {
		const goroutines = 50
		var wg sync.WaitGroup
		results := make([]guard.Result, goroutines)
		for i := 0; i < goroutines; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = guard.Evaluate("git push --force")
			}()
		}
		wg.Wait()
		for i := 1; i < goroutines; i++ {
			if results[i].Decision != results[0].Decision {
				t.Fatalf("decision mismatch at %d: %s vs %s", i, results[i].Decision, results[0].Decision)
			}
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run", "TestStressRapidInitializationSubprocess")
	cmd.Env = append(os.Environ(), "DCG_RAPID_INIT_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rapid init subprocess failed: %v\n%s", err, string(out))
	}
}
