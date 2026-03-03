package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFaultHookInputMatrix(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErr    bool
		wantOutput string
	}{
		{name: "empty-stdin", input: "", wantErr: true},
		{name: "invalid-json", input: "not json", wantErr: true},
		{name: "incomplete-json", input: `{"tool_name":"Bash"`, wantErr: true},
		{name: "missing-tool-input", input: `{"tool_name":"Bash"}`, wantErr: false, wantOutput: "allow"},
		{name: "null-command", input: `{"tool_name":"Bash","tool_input":{"command":null}}`, wantErr: false, wantOutput: "allow"},
		{name: "extra-fields", input: `{"tool_name":"Bash","tool_input":{"command":"ls","extra":42}}`, wantErr: false, wantOutput: "allow"},
		{name: "non-bash", input: `{"tool_name":"Read","tool_input":{"file_path":"/etc/passwd"}}`, wantErr: false, wantOutput: "allow"},
		{name: "unknown-hook-event", input: `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`, wantErr: false, wantOutput: "allow"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			reset := withIO(t)
			defer reset()
			stdin = strings.NewReader(tt.input)
			err := runHookMode()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var out HookOutput
			if err := json.Unmarshal(stdout.(*bytes.Buffer).Bytes(), &out); err != nil {
				t.Fatalf("invalid hook output json: %v", err)
			}
			if out.HookSpecificOutput.PermissionDecision != tt.wantOutput {
				t.Fatalf("decision=%q want=%q", out.HookSpecificOutput.PermissionDecision, tt.wantOutput)
			}
		})
	}
}

func TestFaultHookInputTooLarge(t *testing.T) {
	reset := withIO(t)
	defer reset()

	// Bigger than maxHookInputSize and syntactically incomplete after truncation.
	stdin = strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("a", maxHookInputSize+2048))
	err := runHookMode()
	if err == nil {
		t.Fatal("expected parse error for oversized input")
	}
}

func TestFaultAdversarialConfigParse(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantError bool
	}{
		{name: "binary", content: "\x00\x01\x02", wantError: true},
		{name: "yaml-anchors", content: "a: &a [*a, *a, *a, *a]", wantError: false},
		{name: "very-long-value", content: "allowlist:\n  - \"" + strings.Repeat("x", 100_000) + "\"", wantError: false},
		{name: "special-chars", content: `allowlist: ["git; rm -rf /"]`, wantError: false},
		{name: "policy-int", content: "policy: 42", wantError: false},
		{name: "policy-list", content: "policy: [strict, interactive]", wantError: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseConfig([]byte(tc.content))
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected parse error, got cfg=%+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			_ = cfg.toOptions()
		})
	}
}

func TestSecurityHookOutputNoSecretLeakage(t *testing.T) {
	tests := []struct {
		command   string
		forbidden []string
	}{
		{command: "vault delete secret/production/stripe-api-key", forbidden: []string{"stripe-api-key", "production"}},
		{command: "git push --force token=s3cr3t", forbidden: []string{"s3cr3t", "token=s3cr3t"}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.command, func(t *testing.T) {
			out := processHookInput(HookInput{ToolName: "Bash", ToolInput: ToolInput{Command: tc.command}})
			reason := strings.ToLower(out.HookSpecificOutput.PermissionDecisionReason)
			for _, bad := range tc.forbidden {
				if strings.Contains(reason, strings.ToLower(bad)) {
					t.Fatalf("reason leaked sensitive content %q: %q", bad, out.HookSpecificOutput.PermissionDecisionReason)
				}
			}
		})
	}
}

func TestSecurityParseConfigSizeLimit(t *testing.T) {
	data := bytes.Repeat([]byte("a"), maxConfigFileSize+1)
	if _, err := parseConfig(data); err == nil {
		t.Fatal("expected size limit error")
	}
}
