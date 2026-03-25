package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseTestToolInputBash(t *testing.T) {
	got, err := parseTestToolInput("Bash", "rm -rf /")
	if err != nil {
		t.Fatalf("parseTestToolInput: %v", err)
	}
	if got["command"] != "rm -rf /" {
		t.Fatalf("command=%v want rm -rf /", got["command"])
	}
}

func TestParseTestToolInputNonBashRequiresJSONObject(t *testing.T) {
	if _, err := parseTestToolInput("Read", "not-json"); err == nil {
		t.Fatal("expected error for non-JSON non-Bash input")
	}
}

func TestTestCmdToolReadJSON(t *testing.T) {
	out, _, err := execCmd(t, "test", "--json", "--tool", "Read", `{"file_path":"/Users/testuser/.ssh/id_rsa"}`)
	if err != nil {
		t.Fatalf("test --tool Read: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if result["command"] != "Read(/Users/testuser/.ssh/id_rsa)" {
		t.Fatalf("command=%v", result["command"])
	}
	if result["decision"] == "allow" {
		t.Fatalf("decision=%v want non-allow", result["decision"])
	}
}

func TestTestCmdToolBashMatchesBare(t *testing.T) {
	bareOut, _, err := execCmd(t, "test", "--json", "ls -la")
	if err != nil {
		t.Fatalf("bare test: %v", err)
	}
	toolOut, _, err := execCmd(t, "test", "--json", "--tool", "Bash", "ls -la")
	if err != nil {
		t.Fatalf("tool Bash test: %v", err)
	}
	if bareOut != toolOut {
		t.Fatalf("Bash outputs differ\nbare=%s\ntool=%s", bareOut, toolOut)
	}
}

func TestTestCmdToolBadJSONReturnsError(t *testing.T) {
	_, stderrOut, err := execCmd(t, "test", "--tool", "Read", "not-json")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parsing --tool Read input") && !strings.Contains(stderrOut, "parsing --tool Read input") {
		t.Fatalf("unexpected error: %v", err)
	}
}
