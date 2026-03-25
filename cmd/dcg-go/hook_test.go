package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestDecisionToHookDecision(t *testing.T) {
	if got := decisionToHookDecision(guard.Allow); got != "allow" {
		t.Fatalf("allow => %q", got)
	}
	if got := decisionToHookDecision(guard.Deny); got != "deny" {
		t.Fatalf("deny => %q", got)
	}
	if got := decisionToHookDecision(guard.Ask); got != "ask" {
		t.Fatalf("ask => %q", got)
	}
}

func TestWriteHookOutput(t *testing.T) {
	outBuf := &bytes.Buffer{}
	oldOut := stdout
	stdout = outBuf
	t.Cleanup(func() { stdout = oldOut })

	if err := writeHookOutput("deny", "test reason"); err != nil {
		t.Fatalf("writeHookOutput error: %v", err)
	}

	var out HookOutput
	if err := json.Unmarshal(outBuf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("decision = %q", out.HookSpecificOutput.PermissionDecision)
	}
	if out.HookSpecificOutput.PermissionDecisionReason != "test reason" {
		t.Fatalf("reason = %q", out.HookSpecificOutput.PermissionDecisionReason)
	}
}

func TestRunHookModeNonBashEvaluates(t *testing.T) {
	outBuf := &bytes.Buffer{}
	oldIn, oldOut := stdin, stdout
	stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/Users/testuser/.ssh/id_rsa"}}`)
	stdout = outBuf
	t.Cleanup(func() { stdin, stdout = oldIn, oldOut })

	if err := runHookMode(); err != nil {
		t.Fatalf("runHookMode error: %v", err)
	}

	var out HookOutput
	if err := json.Unmarshal(outBuf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.HookSpecificOutput.PermissionDecision == "allow" {
		t.Fatalf("decision = %q, want non-allow", out.HookSpecificOutput.PermissionDecision)
	}
}

func TestRunHookModeBashDeny(t *testing.T) {
	outBuf := &bytes.Buffer{}
	oldIn, oldOut := stdin, stdout
	stdin = strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
	stdout = outBuf
	t.Cleanup(func() { stdin, stdout = oldIn, oldOut })

	if err := runHookMode(); err != nil {
		t.Fatalf("runHookMode error: %v", err)
	}

	var out HookOutput
	if err := json.Unmarshal(outBuf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("decision = %q, want deny", out.HookSpecificOutput.PermissionDecision)
	}
	if out.HookSpecificOutput.PermissionDecisionReason == "" {
		t.Fatal("expected non-empty deny reason")
	}
}

func TestRunHookModeUnsupportedEventWarnsAndAllows(t *testing.T) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	oldIn, oldOut, oldErr := stdin, stdout, stderr
	stdin = strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
	stdout = outBuf
	stderr = errBuf
	t.Cleanup(func() { stdin, stdout, stderr = oldIn, oldOut, oldErr })

	if err := runHookMode(); err != nil {
		t.Fatalf("runHookMode error: %v", err)
	}

	if !strings.Contains(errBuf.String(), "unsupported hook event") {
		t.Fatalf("stderr missing warning: %q", errBuf.String())
	}
	var out HookOutput
	if err := json.Unmarshal(outBuf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("decision = %q, want allow", out.HookSpecificOutput.PermissionDecision)
	}
}

func TestRunHookModeNonBashFieldsSurviveUnmarshal(t *testing.T) {
	input := `{"hook_event_name":"PreToolUse","tool_name":"Grep","tool_input":{"pattern":"password","path":"/Users/testuser/Documents"}}`

	var hookInput HookInput
	if err := json.Unmarshal([]byte(input), &hookInput); err != nil {
		t.Fatalf("unmarshal hook input: %v", err)
	}

	toolInput, err := decodeHookToolInput(hookInput.ToolInput)
	if err != nil {
		t.Fatalf("decodeHookToolInput: %v", err)
	}
	if got := toolInput["pattern"]; got != "password" {
		t.Fatalf("pattern=%v want password", got)
	}
	if got := toolInput["path"]; got != "/Users/testuser/Documents" {
		t.Fatalf("path=%v want /Users/testuser/Documents", got)
	}
}
