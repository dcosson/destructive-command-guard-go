package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunListPacksHuman(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runListMode([]string{"packs"}); err != nil {
		t.Fatalf("runListMode packs error: %v", err)
	}
	out := stdout.(*bytes.Buffer).String()
	if !strings.Contains(out, "Registered packs") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "core.git") {
		t.Fatalf("missing core.git: %q", out)
	}
	if !strings.Contains(out, "destructive") {
		t.Fatalf("missing destructive count: %q", out)
	}
}

func TestRunListPacksJSON(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runListMode([]string{"packs", "--json"}); err != nil {
		t.Fatalf("runListMode packs --json error: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(stdout.(*bytes.Buffer).Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty packs list")
	}
	// Verify per-category counts exist in JSON
	first := out[0]
	if _, ok := first["DestructiveCount"]; !ok {
		t.Fatal("missing DestructiveCount in JSON output")
	}
	if _, ok := first["PrivacyCount"]; !ok {
		t.Fatal("missing PrivacyCount in JSON output")
	}
	if _, ok := first["BothCount"]; !ok {
		t.Fatal("missing BothCount in JSON output")
	}
}

func TestRunListRulesHuman(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runListMode([]string{"rules"}); err != nil {
		t.Fatalf("runListMode rules error: %v", err)
	}
	out := stdout.(*bytes.Buffer).String()
	if !strings.Contains(out, "Destructive") {
		t.Fatalf("missing Destructive group: %q", out)
	}
	if !strings.Contains(out, "Privacy") {
		t.Fatalf("missing Privacy group: %q", out)
	}
	if !strings.Contains(out, "Both") {
		t.Fatalf("missing Both group: %q", out)
	}
	if !strings.Contains(out, "core.git") {
		t.Fatalf("missing core.git pack reference: %q", out)
	}
}

func TestRunListRulesJSON(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runListMode([]string{"rules", "--json"}); err != nil {
		t.Fatalf("runListMode rules --json error: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(stdout.(*bytes.Buffer).Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty rules list")
	}
	// Verify rule fields exist
	first := out[0]
	for _, field := range []string{"ID", "PackID", "Category", "Severity", "Reason"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("missing %s in JSON output", field)
		}
	}
}

func TestRunListNoSubcommand(t *testing.T) {
	err := runListMode(nil)
	if err == nil {
		t.Fatal("expected error for no subcommand")
	}
}

func TestRunListUnknownSubcommand(t *testing.T) {
	err := runListMode([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}
