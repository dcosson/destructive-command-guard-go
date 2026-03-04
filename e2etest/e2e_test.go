package e2etest

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestE2ERealWorldScenarios(t *testing.T) {
	scenarios := []struct {
		name         string
		command      string
		wantDecision guard.Decision
		wantMinSev   guard.Severity
	}{
		{"force push to main", "git push --force origin main", guard.Deny, guard.High},
		{"production database reset", "RAILS_ENV=production rails db:reset", guard.Deny, guard.Critical},
		{"dev database reset", "RAILS_ENV=development rails db:reset", guard.Deny, guard.High},
		{"variable carries danger", "DIR=/tmp/e2e; rm -rf $DIR", guard.Deny, guard.Critical},
		{"compound git + rm", "git push --force && rm -rf /tmp/e2e", guard.Deny, guard.Critical},
		{"grep dangerous pattern", `grep -r "DROP TABLE" .`, guard.Allow, guard.Indeterminate},
		{"man page safe", "man git-push", guard.Allow, guard.Indeterminate},
		{"empty command", "", guard.Allow, guard.Indeterminate},
		{"whitespace command", "   \t\n  ", guard.Allow, guard.Indeterminate},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			result := guard.Evaluate(sc.command, guard.WithPolicy(guard.InteractivePolicy()))
			if result.Decision != sc.wantDecision {
				t.Fatalf("decision mismatch for %q: got %s want %s", sc.command, result.Decision, sc.wantDecision)
			}
			if sc.wantDecision != guard.Allow && result.Assessment != nil &&
				result.Assessment.Severity < sc.wantMinSev {
				t.Fatalf("severity too low for %q: got %s want >= %s", sc.command, result.Assessment.Severity, sc.wantMinSev)
			}
		})
	}
}

func TestE2EPolicyVariations(t *testing.T) {
	type expectation struct {
		policy guard.Policy
		name   string
		want   guard.Decision
	}
	scenarios := []struct {
		name string
		cmd  string
		exps []expectation
	}{
		{
			name: "high severity command",
			cmd:  "git push --force origin main",
			exps: []expectation{
				{guard.StrictPolicy(), "strict", guard.Deny},
				{guard.InteractivePolicy(), "interactive", guard.Deny},
				{guard.PermissivePolicy(), "permissive", guard.Ask},
			},
		},
		{
			name: "critical severity command",
			cmd:  "rm -rf /tmp/e2e",
			exps: []expectation{
				{guard.StrictPolicy(), "strict", guard.Deny},
				{guard.InteractivePolicy(), "interactive", guard.Deny},
				{guard.PermissivePolicy(), "permissive", guard.Deny},
			},
		},
		{
			name: "safe command",
			cmd:  "echo hello",
			exps: []expectation{
				{guard.StrictPolicy(), "strict", guard.Allow},
				{guard.InteractivePolicy(), "interactive", guard.Allow},
				{guard.PermissivePolicy(), "permissive", guard.Allow},
			},
		},
	}

	for _, sc := range scenarios {
		for _, exp := range sc.exps {
			sc := sc
			exp := exp
			t.Run(sc.name+"/"+exp.name, func(t *testing.T) {
				result := guard.Evaluate(sc.cmd, guard.WithPolicy(exp.policy))
				if result.Decision != exp.want {
					t.Fatalf("policy %s mismatch for %q: got %s want %s", exp.name, sc.cmd, result.Decision, exp.want)
				}
			})
		}
	}
}

func TestE2EHookMode(t *testing.T) {
	binary := buildDCGBinary(t)
	tests := []struct {
		name         string
		input        string
		wantDecision string
		wantExit     int
	}{
		{"deny rm-rf", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /tmp/e2e"}}`, "deny", 0},
		{"allow echo", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hello"}}`, "allow", 0},
		{"allow non-bash", `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/etc/passwd"}}`, "allow", 0},
		{"malformed input", "not json", "", 1},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binary)
			cmd.Stdin = strings.NewReader(tt.input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else {
					t.Fatalf("unexpected run error: %v", err)
				}
			}
			if code != tt.wantExit {
				t.Fatalf("exit code = %d, want %d", code, tt.wantExit)
			}
			if tt.wantExit == 0 {
				if !json.Valid(stdout.Bytes()) {
					t.Fatalf("stdout is not valid json: %q", stdout.String())
				}
				var out map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
					t.Fatalf("unmarshal output: %v", err)
				}
				hs, ok := out["hookSpecificOutput"].(map[string]any)
				if !ok {
					t.Fatalf("missing hookSpecificOutput in %v", out)
				}
				if got := hs["permissionDecision"]; got != tt.wantDecision {
					t.Fatalf("decision = %v, want %s", got, tt.wantDecision)
				}
			}
		})
	}
}

func TestE2ETestMode(t *testing.T) {
	binary := buildDCGBinary(t)
	tests := []struct {
		name         string
		args         []string
		wantExitCode int
		wantContains string
	}{
		{"allow safe command", []string{"test", "echo hello"}, 0, "Decision: Allow"},
		{"deny destructive command", []string{"test", "rm -rf /tmp/e2e"}, 2, "Decision: Deny"},
		{"json output", []string{"test", "--json", "git push --force"}, 2, `"decision": "Deny"`},
		{"explain mode", []string{"test", "--explain", "git push --force"}, 2, "Reason:"},
		{"policy override permissive", []string{"test", "--policy", "permissive", "git push --force"}, 3, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binary, tt.args...)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			err := cmd.Run()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatalf("unexpected run error: %v", err)
			}
			if code != tt.wantExitCode {
				t.Fatalf("exit code = %d, want %d", code, tt.wantExitCode)
			}
			if tt.wantContains != "" && !strings.Contains(stdout.String(), tt.wantContains) {
				t.Fatalf("stdout missing %q: %s", tt.wantContains, stdout.String())
			}
		})
	}
}

func TestE2EPacksMode(t *testing.T) {
	binary := buildDCGBinary(t)
	cmd := exec.Command(binary, "packs", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("packs --json failed: %v", err)
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("packs output not valid json: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "core.git") {
		t.Fatalf("packs output missing core.git: %s", stdout.String())
	}
}

func buildDCGBinary(t *testing.T) string {
	t.Helper()
	root, err := FindModuleRoot()
	if err != nil {
		t.Fatalf("FindModuleRoot: %v", err)
	}
	outDir := t.TempDir()
	bin, err := buildTestBinary("./cmd/dcg-go", outDir, "dcg-go")
	if err == nil {
		return bin
	}
	// Retry with explicit module-root-relative path from this package.
	bin = filepath.Join(outDir, "dcg-go")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/dcg-go")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, string(out))
	}
	return bin
}
