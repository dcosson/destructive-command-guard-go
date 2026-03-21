package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePolicy(t *testing.T) {
	for _, name := range []string{"allow-all", "permissive", "moderate", "strict", "very-strict", "interactive"} {
		if _, err := parsePolicy(name); err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
	}
	if _, err := parsePolicy("wat"); err == nil {
		t.Fatal("expected error for unknown policy")
	}
}

func TestTestModeDenyExitCode(t *testing.T) {
	out, _, err := execCmd(t, "test", "rm -rf /")
	if err != nil {
		t.Fatalf("test error: %v", err)
	}
	if !strings.Contains(out, "Decision: Deny") {
		t.Fatalf("expected Deny: %q", out)
	}
}

func TestTestModeJSONOutput(t *testing.T) {
	out, _, err := execCmd(t, "test", "--json", "git status")
	if err != nil {
		t.Fatalf("test --json error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json invalid: %v\n%s", err, out)
	}
	if result["command"] != "git status" {
		t.Fatalf("command = %#v", result["command"])
	}
}

func TestTestModeExplainOutput(t *testing.T) {
	out, _, err := execCmd(t, "test", "--explain", "rm -rf /")
	if err != nil {
		t.Fatalf("test --explain error: %v", err)
	}
	if !strings.Contains(out, "Decision: Deny") {
		t.Fatalf("missing decision: %q", out)
	}
	if !strings.Contains(out, "Reason:") {
		t.Fatalf("missing explain reason: %q", out)
	}
}

func TestTestModeNoArgs(t *testing.T) {
	_, _, err := execCmd(t, "test")
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestTestModeBlocklist(t *testing.T) {
	out, _, err := execCmd(t, "test", "--blocklist", "echo *", "echo hello")
	if err != nil {
		t.Fatalf("test --blocklist error: %v", err)
	}
	if !strings.Contains(out, "Decision: Deny") {
		t.Fatalf("expected Deny with blocklist: %q", out)
	}
}

func TestTestModeAllowlist(t *testing.T) {
	out, _, err := execCmd(t, "test", "--allowlist", "rm *", "rm -rf /")
	if err != nil {
		t.Fatalf("test --allowlist error: %v", err)
	}
	if !strings.Contains(out, "Decision: Allow") {
		t.Fatalf("expected Allow with allowlist: %q", out)
	}
}
