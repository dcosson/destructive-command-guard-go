//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPropertyProcessHookInputDeterminism(t *testing.T) {
	cases := []HookInput{
		{ToolName: "Bash", ToolInput: rawToolInput(t, map[string]any{"command": "echo hello"})},
		{ToolName: "Bash", ToolInput: rawToolInput(t, map[string]any{"command": "rm -rf /"})},
		{ToolName: "Read", ToolInput: rawToolInput(t, map[string]any{"file_path": "/tmp/x"})},
		{ToolName: "Bash", ToolInput: rawToolInput(t, map[string]any{"command": ""})},
		{HookEventName: "PostToolUse", ToolName: "Bash", ToolInput: rawToolInput(t, map[string]any{"command": "rm -rf /"})},
	}

	for i, in := range cases {
		in := in
		t.Run("case-"+string(rune('a'+i)), func(t *testing.T) {
			reset := withIO(t)
			defer reset()
			o1 := processHookInput(in)
			o2 := processHookInput(in)
			if o1.HookSpecificOutput.PermissionDecision != o2.HookSpecificOutput.PermissionDecision {
				t.Fatalf("decision mismatch: %q vs %q", o1.HookSpecificOutput.PermissionDecision, o2.HookSpecificOutput.PermissionDecision)
			}
			if o1.HookSpecificOutput.HookEventName != "PreToolUse" || o2.HookSpecificOutput.HookEventName != "PreToolUse" {
				t.Fatalf("hook event must be PreToolUse")
			}
		})
	}
}

func TestDeterministicHookModeExamples(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "allow-safe", json: `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hello"}}`, want: "allow"},
		{name: "deny-destructive", json: `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`, want: "deny"},
		{name: "allow-nonbash", json: `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/x"}}`, want: "allow"},
		{name: "allow-empty", json: `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":""}}`, want: "allow"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			reset := withIO(t)
			defer reset()
			stdin = strings.NewReader(tc.json)
			if err := runHookMode(); err != nil {
				t.Fatalf("runHookMode error: %v", err)
			}
			var out HookOutput
			if err := json.Unmarshal(stdout.(*bytes.Buffer).Bytes(), &out); err != nil {
				t.Fatalf("invalid hook output json: %v", err)
			}
			if out.HookSpecificOutput.PermissionDecision != tc.want {
				t.Fatalf("decision=%q want=%q", out.HookSpecificOutput.PermissionDecision, tc.want)
			}
		})
	}
}

func TestPropertyPacksModeDeterminism(t *testing.T) {
	first, _, err := execCmd(t, "list", "packs", "--json")
	if err != nil {
		t.Fatalf("list packs --json: %v", err)
	}
	second, _, err := execCmd(t, "list", "packs", "--json")
	if err != nil {
		t.Fatalf("list packs --json (second): %v", err)
	}
	if first != second {
		t.Fatalf("packs --json output changed between runs")
	}
}

func TestDeterministicConfigPathPrecedence(t *testing.T) {
	reset := withIO(t)
	defer reset()

	home := t.TempDir()
	homeDefault := filepath.Join(home, ".config", "dcg-go", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(homeDefault), 0o755); err != nil {
		t.Fatalf("mkdir default config dir: %v", err)
	}
	if err := os.WriteFile(homeDefault, []byte("policy: permissive\n"), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	explicit := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(explicit, []byte("destructive_policy: strict\n"), 0o644); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("DCG_CONFIG", explicit)
	cfg := loadConfig()
	if cfg.DestructivePolicy != "strict" {
		t.Fatalf("explicit DCG_CONFIG should win; got destructive_policy=%q", cfg.DestructivePolicy)
	}
}

func TestDeterministicRunTestModeJSONShape(t *testing.T) {
	outStr, _, err := execCmd(t, "test", "--json", "echo hello")
	if err != nil {
		t.Fatalf("test --json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(outStr), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out["command"] != "echo hello" {
		t.Fatalf("command=%#v want echo hello", out["command"])
	}
	if _, ok := out["decision"]; !ok {
		t.Fatal("missing decision field")
	}
}
