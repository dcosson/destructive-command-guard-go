package guard

import (
	"testing"
)

func TestEvaluateToolUse_Bash_MatchesEvaluate(t *testing.T) {
	commands := []string{
		"",
		"ls -la",
		"rm -rf /",
		"git push --force",
		"echo hello",
		"cat ~/.ssh/id_rsa",
	}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			direct := Evaluate(cmd,
				WithDestructivePolicy(InteractivePolicy()),
				WithPrivacyPolicy(InteractivePolicy()),
			)
			toolUse := EvaluateToolUse("Bash", map[string]any{"command": cmd},
				WithDestructivePolicy(InteractivePolicy()),
				WithPrivacyPolicy(InteractivePolicy()),
			)
			if direct.Decision != toolUse.Decision {
				t.Errorf("Decision mismatch: Evaluate=%v, EvaluateToolUse=%v", direct.Decision, toolUse.Decision)
			}
			if len(direct.Matches) != len(toolUse.Matches) {
				t.Errorf("Matches count mismatch: Evaluate=%d, EvaluateToolUse=%d", len(direct.Matches), len(toolUse.Matches))
			}
			if direct.Reason() != toolUse.Reason() {
				t.Errorf("Reason mismatch: Evaluate=%q, EvaluateToolUse=%q", direct.Reason(), toolUse.Reason())
			}
		})
	}
}

func TestEvaluateToolUse_Read_PrivacySensitivePath(t *testing.T) {
	result := EvaluateToolUse("Read", map[string]any{
		"file_path": "/Users/testuser/.ssh/id_rsa",
	},
		WithDestructivePolicy(InteractivePolicy()),
		WithPrivacyPolicy(InteractivePolicy()),
	)
	// SSH key access should trigger privacy rules.
	if result.Decision == Allow {
		t.Errorf("expected non-Allow for reading SSH key, got %v", result.Decision)
	}
}

func TestEvaluateToolUse_Write_PrivacySensitivePath(t *testing.T) {
	result := EvaluateToolUse("Write", map[string]any{
		"file_path": "/Users/testuser/Documents/secrets.txt",
	},
		WithDestructivePolicy(InteractivePolicy()),
		WithPrivacyPolicy(InteractivePolicy()),
	)
	// Writing to ~/Documents should trigger privacy rules.
	if result.Decision == Allow {
		t.Errorf("expected non-Allow for writing to Documents, got %v", result.Decision)
	}
}

func TestEvaluateToolUse_Read_SafePath(t *testing.T) {
	result := EvaluateToolUse("Read", map[string]any{
		"file_path": "/tmp/test.txt",
	},
		WithDestructivePolicy(InteractivePolicy()),
		WithPrivacyPolicy(InteractivePolicy()),
	)
	if result.Decision != Allow {
		t.Errorf("expected Allow for reading /tmp/test.txt, got %v", result.Decision)
	}
}

func TestEvaluateToolUse_UnknownTool_Allow(t *testing.T) {
	result := EvaluateToolUse("FutureTool", map[string]any{"foo": "bar"})
	if result.Decision != Allow {
		t.Errorf("expected Allow for unknown tool, got %v", result.Decision)
	}
	if result.Command != "FutureTool" {
		t.Errorf("Command = %q, want %q", result.Command, "FutureTool")
	}
}

func TestEvaluateToolUse_Agent_NoEval(t *testing.T) {
	result := EvaluateToolUse("Agent", map[string]any{"prompt": "do stuff"})
	if result.Decision != Allow {
		t.Errorf("expected Allow for Agent (NoEval), got %v", result.Decision)
	}
}

func TestEvaluateToolUse_Read_MissingPath_Indeterminate(t *testing.T) {
	result := EvaluateToolUse("Read", map[string]any{},
		WithDestructivePolicy(InteractivePolicy()),
		WithPrivacyPolicy(InteractivePolicy()),
	)
	// Missing file_path on a known tool → indeterminate → Ask with interactive policy.
	if result.Decision != Ask {
		t.Errorf("expected Ask for Read with missing file_path, got %v", result.Decision)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning for missing file_path")
	}
}

func TestEvaluateToolUse_Read_WrongType_Indeterminate(t *testing.T) {
	result := EvaluateToolUse("Read", map[string]any{"file_path": 123},
		WithDestructivePolicy(InteractivePolicy()),
		WithPrivacyPolicy(InteractivePolicy()),
	)
	if result.Decision != Ask {
		t.Errorf("expected Ask for Read with wrong type file_path, got %v", result.Decision)
	}
}

func TestEvaluateToolUse_Read_StrictPolicy_MissingPath_Deny(t *testing.T) {
	result := EvaluateToolUse("Read", map[string]any{},
		WithDestructivePolicy(StrictPolicy()),
		WithPrivacyPolicy(StrictPolicy()),
	)
	// Strict denies Indeterminate.
	if result.Decision != Deny {
		t.Errorf("expected Deny for Read with missing file_path under strict, got %v", result.Decision)
	}
}

func TestEvaluateToolUse_ResultCommand(t *testing.T) {
	tests := []struct {
		tool    string
		input   map[string]any
		wantCmd string
	}{
		{"Read", map[string]any{"file_path": "/foo"}, "Read(/foo)"},
		{"Write", map[string]any{"file_path": "/bar"}, "Write(/bar)"},
		{"Grep", map[string]any{"pattern": "x", "path": "/baz"}, "Grep(x, /baz)"},
		{"Agent", map[string]any{}, "Agent"},
		{"FutureTool", map[string]any{}, "FutureTool"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := EvaluateToolUse(tt.tool, tt.input)
			if result.Command != tt.wantCmd {
				t.Errorf("Command = %q, want %q", result.Command, tt.wantCmd)
			}
		})
	}
}

func TestEvaluateToolUse_Blocklist_MatchesBashEquivalent(t *testing.T) {
	// A blocklist pattern "cat *" should block both Evaluate("cat /foo") and
	// EvaluateToolUse("Read", {"file_path": "/foo"}).
	bashResult := Evaluate("cat /foo", WithBlocklist("cat *"))
	toolResult := EvaluateToolUse("Read", map[string]any{"file_path": "/foo"}, WithBlocklist("cat *"))

	if bashResult.Decision != Deny {
		t.Errorf("Bash blocklist: expected Deny, got %v", bashResult.Decision)
	}
	if toolResult.Decision != Deny {
		t.Errorf("ToolUse blocklist: expected Deny, got %v", toolResult.Decision)
	}
}

func TestTools(t *testing.T) {
	tools := Tools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool catalog")
	}
	// Check a known tool.
	found := false
	for _, tool := range tools {
		if tool.ToolName == "Read" {
			found = true
			if tool.SyntheticCommand != "cat" {
				t.Errorf("Read.SyntheticCommand = %q, want %q", tool.SyntheticCommand, "cat")
			}
			if tool.PathField != "file_path" {
				t.Errorf("Read.PathField = %q, want %q", tool.PathField, "file_path")
			}
		}
	}
	if !found {
		t.Error("Read not found in tool catalog")
	}
}
