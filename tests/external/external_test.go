// Package external runs black-box tests against the compiled dcg-go binary.
// These tests build the real binary and invoke it as a subprocess, validating
// CLI output and exit codes with various policy configurations.
package external

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	binaryOnce sync.Once
	binaryPath string
	buildErr   error
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "dcg-external-test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "dcg-go")
	cmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/dcg-go")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build dcg-go: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func binary(t *testing.T) string {
	t.Helper()
	return binaryPath
}

type testResult struct {
	Command                string `json:"command"`
	Decision               string `json:"decision"`
	DestructiveSeverity    string `json:"destructive_severity,omitempty"`
	DestructiveConfidence  string `json:"destructive_confidence,omitempty"`
	PrivacySeverity        string `json:"privacy_severity,omitempty"`
	PrivacyConfidence      string `json:"privacy_confidence,omitempty"`
}

func runTest(t *testing.T, bin string, args ...string) (testResult, int) {
	t.Helper()
	fullArgs := append([]string{"test", "--json"}, args...)
	cmd := exec.Command(bin, fullArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("run error: %v\nstderr: %s", err, stderr.String())
		}
	}
	var result testResult
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
			t.Fatalf("invalid JSON output: %v\nraw: %s", jsonErr, stdout.String())
		}
	}
	return result, exitCode
}

// Exit codes: 0=Allow, 2=Deny, 3=Ask
const (
	exitAllow = 0
	exitDeny  = 2
	exitAsk   = 3
)

func TestExternalSafeCommands(t *testing.T) {
	bin := binary(t)
	safe := []string{
		"echo hello",
		"git status",
		"ls -la",
		"cat README.md",
		"pwd",
	}
	for _, cmd := range safe {
		t.Run(cmd, func(t *testing.T) {
			result, exit := runTest(t, bin, cmd)
			if exit != exitAllow {
				t.Fatalf("expected exit 0 (Allow), got %d for %q", exit, cmd)
			}
			if result.Decision != "Allow" {
				t.Fatalf("expected Allow, got %s for %q", result.Decision, cmd)
			}
		})
	}
}

func TestExternalDestructiveDefaultPolicy(t *testing.T) {
	bin := binary(t)
	// Default policy is Interactive — High severity should Ask.
	cases := []struct {
		cmd      string
		wantExit int
		wantDec  string
	}{
		{"rm -rf /", exitDeny, "Deny"},         // Critical → Deny
		{"git push --force", exitAsk, "Ask"},    // High → Ask
		{"echo hello", exitAllow, "Allow"},      // No match → Allow
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			result, exit := runTest(t, bin, tc.cmd)
			if exit != tc.wantExit {
				t.Fatalf("exit=%d want=%d for %q (decision=%s)", exit, tc.wantExit, tc.cmd, result.Decision)
			}
			if result.Decision != tc.wantDec {
				t.Fatalf("decision=%s want=%s for %q", result.Decision, tc.wantDec, tc.cmd)
			}
		})
	}
}

func TestExternalPolicyVariations(t *testing.T) {
	bin := binary(t)
	// git push --force is High severity, Destructive category.
	cmd := "git push --force"
	cases := []struct {
		policy   string
		wantExit int
		wantDec  string
	}{
		{"allow-all", exitAllow, "Allow"},
		{"permissive", exitAllow, "Allow"},    // Permissive allows High
		{"moderate", exitDeny, "Deny"},         // Moderate denies High
		{"strict", exitDeny, "Deny"},           // Strict denies all
		{"interactive", exitAsk, "Ask"},        // Interactive asks for High
	}
	for _, tc := range cases {
		t.Run(tc.policy, func(t *testing.T) {
			result, exit := runTest(t, bin, "--destructive-policy", tc.policy, cmd)
			if exit != tc.wantExit {
				t.Fatalf("exit=%d want=%d for policy=%s (decision=%s)", exit, tc.wantExit, tc.policy, result.Decision)
			}
			if result.Decision != tc.wantDec {
				t.Fatalf("decision=%s want=%s for policy=%s", result.Decision, tc.wantDec, tc.policy)
			}
		})
	}
}

func TestExternalDualPolicySplit(t *testing.T) {
	bin := binary(t)

	// Destructive-permissive + privacy-strict:
	// git push --force (destructive High) should be allowed by permissive.
	t.Run("destructive-allowed-by-permissive", func(t *testing.T) {
		result, exit := runTest(t, bin,
			"--destructive-policy", "permissive",
			"--privacy-policy", "strict",
			"git push --force")
		if exit != exitAllow {
			t.Fatalf("expected Allow for destructive-permissive, got exit=%d decision=%s", exit, result.Decision)
		}
	})

	// Destructive-permissive + privacy-strict:
	// Critical destructive command should still be denied.
	t.Run("critical-denied-even-permissive", func(t *testing.T) {
		result, exit := runTest(t, bin,
			"--destructive-policy", "permissive",
			"--privacy-policy", "strict",
			"rm -rf /")
		if exit != exitDeny {
			t.Fatalf("expected Deny for critical, got exit=%d decision=%s", exit, result.Decision)
		}
	})

	// Policy shorthand sets both.
	t.Run("policy-shorthand-sets-both", func(t *testing.T) {
		result, exit := runTest(t, bin,
			"--policy", "strict",
			"git push --force")
		if exit != exitDeny {
			t.Fatalf("expected Deny for strict, got exit=%d decision=%s", exit, result.Decision)
		}
	})
}

func TestExternalPerCategoryAssessments(t *testing.T) {
	bin := binary(t)

	// Destructive command should have destructive assessment, no privacy assessment.
	t.Run("destructive-only", func(t *testing.T) {
		result, _ := runTest(t, bin, "git push --force")
		if result.DestructiveSeverity == "" {
			t.Fatal("expected destructive severity for destructive command")
		}
		if result.PrivacySeverity != "" {
			t.Fatalf("unexpected privacy severity for destructive command: %s", result.PrivacySeverity)
		}
	})
}

func TestExternalListPacks(t *testing.T) {
	bin := binary(t)
	cmd := exec.Command(bin, "list", "packs", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("list packs --json failed: %v", err)
	}
	var packs []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &packs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(packs) == 0 {
		t.Fatal("expected non-empty packs list")
	}
	// Verify core.git exists.
	found := false
	for _, p := range packs {
		if p["ID"] == "core.git" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("core.git pack not found")
	}
}

func TestExternalListRules(t *testing.T) {
	bin := binary(t)
	cmd := exec.Command(bin, "list", "rules", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("list rules --json failed: %v", err)
	}
	var rules []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rules); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(rules) < 100 {
		t.Fatalf("expected 100+ rules, got %d", len(rules))
	}
	// Verify category field exists.
	for _, r := range rules[:5] {
		if _, ok := r["Category"]; !ok {
			t.Fatalf("missing Category field in rule: %v", r)
		}
	}
}

func TestExternalHookMode(t *testing.T) {
	bin := binary(t)
	cases := []struct {
		name     string
		command  string
		wantPerm string
	}{
		{"deny-critical", "rm -rf /", "deny"},
		{"allow-safe", "echo hello", "allow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"%s"}}`, tc.command)
			cmd := exec.Command(bin)
			cmd.Stdin = strings.NewReader(input)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("hook mode failed: %v", err)
			}
			var output map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			hso, ok := output["hookSpecificOutput"].(map[string]any)
			if !ok {
				t.Fatalf("missing hookSpecificOutput: %v", output)
			}
			if got := hso["permissionDecision"]; got != tc.wantPerm {
				t.Fatalf("permissionDecision=%v want=%s", got, tc.wantPerm)
			}
		})
	}
}

func TestExternalVersionAndHelp(t *testing.T) {
	bin := binary(t)

	t.Run("version", func(t *testing.T) {
		out, err := exec.Command(bin, "version").Output()
		if err != nil {
			t.Fatalf("version failed: %v", err)
		}
		if !strings.HasPrefix(string(out), "dcg-go ") {
			t.Fatalf("unexpected version output: %s", out)
		}
	})

	t.Run("help", func(t *testing.T) {
		out, err := exec.Command(bin, "help").Output()
		if err != nil {
			t.Fatalf("help failed: %v", err)
		}
		if !strings.Contains(string(out), "list packs") {
			t.Fatalf("help missing list packs: %s", out)
		}
	})
}
