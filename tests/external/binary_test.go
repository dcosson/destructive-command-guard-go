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

// TestExternalToolUseTestMode exercises the --tool flag for every tool in
// the catalog via the test subcommand. Each tool gets:
//   - An allow case (safe path/input)
//   - A privacy failure case (sensitive path like ~/Documents)
//   - A destructive failure case where applicable
func TestExternalToolUseTestMode(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantExitCode int
		wantContains string
	}{
		// Read: privacy only (cat doesn't match destructive rules)
		{"read-allow", []string{"test", "--tool", "Read", `{"file_path":"/tmp/safe.txt"}`}, 0, "Decision: Allow"},
		{"read-privacy-deny", []string{"test", "--tool", "Read", "--privacy-policy", "strict", `{"file_path":"/Users/testuser/Documents/foo.pdf"}`}, 2, "Decision: Deny"},

		// Write: privacy + destructive
		{"write-allow", []string{"test", "--tool", "Write", `{"file_path":"/tmp/out.txt"}`}, 0, "Decision: Allow"},
		{"write-privacy-deny", []string{"test", "--tool", "Write", "--privacy-policy", "strict", `{"file_path":"/Users/testuser/Documents/report.docx"}`}, 2, "Decision: Deny"},

		// Edit: privacy + destructive
		{"edit-allow", []string{"test", "--tool", "Edit", `{"file_path":"/tmp/config.yaml"}`}, 0, "Decision: Allow"},
		{"edit-privacy-deny", []string{"test", "--tool", "Edit", "--privacy-policy", "strict", `{"file_path":"/Users/testuser/Documents/notes.md"}`}, 2, "Decision: Deny"},

		// Grep: privacy (searches sensitive dirs)
		{"grep-allow", []string{"test", "--tool", "Grep", `{"pattern":"TODO","path":"/tmp/project"}`}, 0, "Decision: Allow"},
		{"grep-privacy-deny", []string{"test", "--tool", "Grep", "--privacy-policy", "strict", `{"pattern":"password","path":"/Users/testuser/Documents"}`}, 2, "Decision: Deny"},

		// Glob: privacy (scans sensitive dirs)
		{"glob-allow", []string{"test", "--tool", "Glob", `{"pattern":"*.go","path":"/tmp/src"}`}, 0, "Decision: Allow"},
		{"glob-privacy-deny", []string{"test", "--tool", "Glob", "--privacy-policy", "strict", `{"pattern":"*.pdf","path":"/Users/testuser/Documents"}`}, 2, "Decision: Deny"},

		// NotebookEdit: privacy
		{"notebook-allow", []string{"test", "--tool", "NotebookEdit", `{"file_path":"/tmp/analysis.ipynb"}`}, 0, "Decision: Allow"},
		{"notebook-privacy-deny", []string{"test", "--tool", "NotebookEdit", "--privacy-policy", "strict", `{"file_path":"/Users/testuser/Documents/research.ipynb"}`}, 2, "Decision: Deny"},

		// WebFetch: allow (curl to safe URL)
		{"webfetch-allow", []string{"test", "--tool", "WebFetch", `{"url":"https://example.com/api"}`}, 0, "Decision: Allow"},

		// Agent: always allow (NoEval)
		{"agent-allow", []string{"test", "--tool", "Agent", `{"prompt":"do stuff"}`}, 0, "Decision: Allow"},

		// WebSearch: always allow (NoEval)
		{"websearch-allow", []string{"test", "--tool", "WebSearch", `{"query":"golang testing"}`}, 0, "Decision: Allow"},

		// Unknown tool: allow
		{"unknown-tool-allow", []string{"test", "--tool", "FutureTool", `{"whatever":"value"}`}, 0, "Decision: Allow"},

		// Bash via --tool flag matches bare command
		{"bash-tool-flag", []string{"test", "--tool", "Bash", "echo hello"}, 0, "Decision: Allow"},
		{"bash-destructive", []string{"test", "--tool", "Bash", "rm -rf /"}, 2, "Decision: Deny"},

		// Normalization error: known tool, missing required field
		{"read-missing-path", []string{"test", "--tool", "Read", `{}`}, 3, "Decision: Ask"},

		// Non-JSON input for non-Bash tool
		{"read-non-json", []string{"test", "--tool", "Read", "not-json"}, 1, ""},
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
				t.Fatalf("exit=%d want=%d stdout=%s", exit, tt.wantExitCode, stdout.String())
			}
			if tt.wantContains != "" && !strings.Contains(stdout.String(), tt.wantContains) {
				t.Fatalf("stdout missing %q:\n%s", tt.wantContains, stdout.String())
			}
		})
	}
}

// TestExternalToolUseHookMode exercises hook mode for non-Bash tools.
func TestExternalToolUseHookMode(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantPerm string
	}{
		// Read safe path
		{"hook-read-allow", `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/safe.txt"}}`, "allow"},
		// Read privacy-sensitive path
		{"hook-read-privacy", `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/Users/testuser/Documents/foo.pdf"}}`, "ask"},
		// Write safe path
		{"hook-write-allow", `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/tmp/out.txt"}}`, "allow"},
		// Write privacy-sensitive path
		{"hook-write-privacy", `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/Users/testuser/Documents/secret.txt"}}`, "ask"},
		// Edit safe path
		{"hook-edit-allow", `{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"/tmp/config.yaml"}}`, "allow"},
		// Grep safe path
		{"hook-grep-allow", `{"hook_event_name":"PreToolUse","tool_name":"Grep","tool_input":{"pattern":"TODO","path":"/tmp/src"}}`, "allow"},
		// Glob safe path
		{"hook-glob-allow", `{"hook_event_name":"PreToolUse","tool_name":"Glob","tool_input":{"pattern":"*.go","path":"/tmp/src"}}`, "allow"},
		// Agent (NoEval)
		{"hook-agent-allow", `{"hook_event_name":"PreToolUse","tool_name":"Agent","tool_input":{"prompt":"do stuff"}}`, "allow"},
		// Unknown tool
		{"hook-unknown-allow", `{"hook_event_name":"PreToolUse","tool_name":"FutureTool","tool_input":{"foo":"bar"}}`, "allow"},
		// Bash destructive
		{"hook-bash-deny", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`, "deny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath)
			cmd.Stdin = strings.NewReader(tc.input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err != nil {
				if _, ok := err.(*exec.ExitError); !ok {
					t.Fatalf("hook mode failed: %v stderr=%s", err, stderr.String())
				}
			}
			var output map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatalf("invalid JSON output: %v\nstdout=%s", err, stdout.String())
			}
			hso, ok := output["hookSpecificOutput"].(map[string]any)
			if !ok {
				t.Fatalf("missing hookSpecificOutput: %v", output)
			}
			if got := hso["permissionDecision"]; got != tc.wantPerm {
				t.Fatalf("permissionDecision=%v want=%s\nstdout=%s", got, tc.wantPerm, stdout.String())
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
