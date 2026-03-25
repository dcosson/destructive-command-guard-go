package external

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binaryPath string

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

type cliResult struct {
	Command               string `json:"command"`
	Decision              string `json:"decision"`
	DestructiveSeverity   string `json:"destructive_severity,omitempty"`
	DestructiveConfidence string `json:"destructive_confidence,omitempty"`
	PrivacySeverity       string `json:"privacy_severity,omitempty"`
	PrivacyConfidence     string `json:"privacy_confidence,omitempty"`
}

func runCLI(t *testing.T, args ...string) (cliResult, int) {
	t.Helper()
	fullArgs := append([]string{"test", "--json"}, args...)
	cmd := exec.Command(binaryPath, fullArgs...)
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
	var result cliResult
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &result); jsonErr != nil {
			t.Fatalf("invalid JSON output: %v\nraw: %s", jsonErr, stdout.String())
		}
	}
	return result, exitCode
}

// Exit codes: 0=Allow, 2=Deny, 3=Ask
func wantExit(decision string) int {
	switch decision {
	case "Deny":
		return 2
	case "Ask":
		return 3
	default:
		return 0
	}
}

func TestBinarySafeCommands(t *testing.T) {
	for _, cmd := range SafeCommands {
		t.Run(cmd, func(t *testing.T) {
			result, exit := runCLI(t, cmd)
			if exit != 0 {
				t.Fatalf("expected exit 0 (Allow), got %d for %q", exit, cmd)
			}
			if result.Decision != "Allow" {
				t.Fatalf("expected Allow, got %s for %q", result.Decision, cmd)
			}
		})
	}
}

func TestBinaryDefaultPolicy(t *testing.T) {
	for _, tc := range DefaultPolicyCases {
		t.Run(tc.Name, func(t *testing.T) {
			result, exit := runCLI(t, tc.Command)
			want := wantExit(tc.WantDecision)
			if exit != want {
				t.Fatalf("exit=%d want=%d for %q (decision=%s)", exit, want, tc.Command, result.Decision)
			}
			if result.Decision != tc.WantDecision {
				t.Fatalf("decision=%s want=%s for %q", result.Decision, tc.WantDecision, tc.Command)
			}
		})
	}
}

func TestBinaryPolicyVariations(t *testing.T) {
	for _, tc := range PolicyVariations {
		t.Run(tc.Policy, func(t *testing.T) {
			result, exit := runCLI(t, "--destructive-policy", tc.Policy, PolicyVariationCommand)
			want := wantExit(tc.WantDecision)
			if exit != want {
				t.Fatalf("exit=%d want=%d for policy=%s (decision=%s)", exit, want, tc.Policy, result.Decision)
			}
			if result.Decision != tc.WantDecision {
				t.Fatalf("decision=%s want=%s for policy=%s", result.Decision, tc.WantDecision, tc.Policy)
			}
		})
	}
}

func TestBinaryDualPolicySplit(t *testing.T) {
	t.Run("destructive-allowed-by-permissive", func(t *testing.T) {
		result, exit := runCLI(t,
			"--destructive-policy", "permissive",
			"--privacy-policy", "strict",
			"git push --force")
		if exit != 0 {
			t.Fatalf("expected Allow for destructive-permissive, got exit=%d decision=%s", exit, result.Decision)
		}
	})

	t.Run("critical-denied-even-permissive", func(t *testing.T) {
		result, exit := runCLI(t,
			"--destructive-policy", "permissive",
			"--privacy-policy", "strict",
			"rm -rf /")
		if exit != 2 {
			t.Fatalf("expected Deny for critical, got exit=%d decision=%s", exit, result.Decision)
		}
	})

	t.Run("policy-shorthand-sets-both", func(t *testing.T) {
		result, exit := runCLI(t, "--destructive-policy", "strict", "--privacy-policy", "strict", "git push --force")
		if exit != 2 {
			t.Fatalf("expected Deny for strict, got exit=%d decision=%s", exit, result.Decision)
		}
	})
}

func TestBinaryPerCategoryAssessments(t *testing.T) {
	t.Run("destructive-only", func(t *testing.T) {
		result, _ := runCLI(t, "git push --force")
		if result.DestructiveSeverity == "" {
			t.Fatal("expected destructive severity for destructive command")
		}
		if result.PrivacySeverity != "" {
			t.Fatalf("unexpected privacy severity for destructive command: %s", result.PrivacySeverity)
		}
	})
}

func TestBinaryListPacks(t *testing.T) {
	cmd := exec.Command(binaryPath, "list", "packs", "--json")
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

func TestBinaryListRules(t *testing.T) {
	cmd := exec.Command(binaryPath, "list", "rules", "--json")
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
	for _, r := range rules[:5] {
		if _, ok := r["Category"]; !ok {
			t.Fatalf("missing Category field in rule: %v", r)
		}
	}
}

func TestBinaryHookMode(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantPerm string
		wantExit int
	}{
		{"deny-critical", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`, "deny", 0},
		{"allow-safe", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hello"}}`, "allow", 0},
		{"allow-non-bash", `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/etc/passwd"}}`, "allow", 0},
		{"malformed-input", `not json`, "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath)
			cmd.Stdin = strings.NewReader(tc.input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			exit := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					exit = ee.ExitCode()
				} else {
					t.Fatalf("hook mode failed: %v", err)
				}
			}
			if exit != tc.wantExit {
				t.Fatalf("exit=%d want=%d stderr=%s", exit, tc.wantExit, stderr.String())
			}
			if tc.wantExit != 0 {
				return
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

func TestBinaryTestMode(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantExitCode int
		wantContains string
	}{
		{"allow-safe-command", []string{"test", "echo hello"}, 0, "Decision: Allow"},
		{"deny-destructive-command", []string{"test", "rm -rf /tmp/e2e"}, 2, "Decision: Deny"},
		{"json-output", []string{"test", "--json", "git push --force"}, 3, `"decision": "Ask"`},
		{"reason-always-shown", []string{"test", "git push --force"}, 3, "Reason:"},
		{"policy-override-permissive", []string{"test", "--destructive-policy", "permissive", "git push --force"}, 0, "Decision: Allow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath, tt.args...)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			err := cmd.Run()
			exit := 0
			if ee, ok := err.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("unexpected run error: %v", err)
			}
			if exit != tt.wantExitCode {
				t.Fatalf("exit=%d want=%d", exit, tt.wantExitCode)
			}
			if tt.wantContains != "" && !strings.Contains(stdout.String(), tt.wantContains) {
				t.Fatalf("stdout missing %q: %s", tt.wantContains, stdout.String())
			}
		})
	}
}

func TestBinaryVersionAndHelp(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		out, err := exec.Command(binaryPath, "version").Output()
		if err != nil {
			t.Fatalf("version failed: %v", err)
		}
		if !strings.HasPrefix(string(out), "dcg-go ") {
			t.Fatalf("unexpected version output: %s", out)
		}
	})

	t.Run("help", func(t *testing.T) {
		out, err := exec.Command(binaryPath, "help").Output()
		if err != nil {
			t.Fatalf("help failed: %v", err)
		}
		if !strings.Contains(string(out), "list") {
			t.Fatalf("help missing list command: %s", out)
		}
	})
}
