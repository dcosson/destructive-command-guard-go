package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePolicy(t *testing.T) {
	if _, err := parsePolicy("strict"); err != nil {
		t.Fatalf("strict error: %v", err)
	}
	if _, err := parsePolicy("interactive"); err != nil {
		t.Fatalf("interactive error: %v", err)
	}
	if _, err := parsePolicy("permissive"); err != nil {
		t.Fatalf("permissive error: %v", err)
	}
	if _, err := parsePolicy("wat"); err == nil {
		t.Fatal("expected error for unknown policy")
	}
}

func TestRunTestModeDenyExitCode(t *testing.T) {
	reset := withIO(t)
	defer reset()

	code := 0
	exitFn = func(c int) { code = c }
	if err := runTestMode([]string{"rm -rf /"}); err != nil {
		t.Fatalf("runTestMode error: %v", err)
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRunTestModeJSONOutput(t *testing.T) {
	reset := withIO(t)
	defer reset()

	exitFn = func(int) {}
	if err := runTestMode([]string{"--json", "git status"}); err != nil {
		t.Fatalf("runTestMode error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.(*bytes.Buffer).Bytes(), &out); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, stdout.(*bytes.Buffer).String())
	}
	if out["command"] != "git status" {
		t.Fatalf("command = %#v", out["command"])
	}
}

func TestRunTestModeExplainOutput(t *testing.T) {
	reset := withIO(t)
	defer reset()

	exitFn = func(int) {}
	if err := runTestMode([]string{"--explain", "rm -rf /"}); err != nil {
		t.Fatalf("runTestMode error: %v", err)
	}
	s := stdout.(*bytes.Buffer).String()
	if !strings.Contains(s, "Decision: Deny") {
		t.Fatalf("output missing decision: %q", s)
	}
	if !strings.Contains(s, "Reason:") {
		t.Fatalf("output missing explain reason: %q", s)
	}
}

func TestRunTestModeUsageError(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runTestMode(nil); err == nil {
		t.Fatal("expected usage error")
	}
}
