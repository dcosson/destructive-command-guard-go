package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestListPacksHuman(t *testing.T) {
	out, _, err := execCmd(t, "list", "packs")
	if err != nil {
		t.Fatalf("list packs error: %v", err)
	}
	if !strings.Contains(out, "Registered packs") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "core.git") {
		t.Fatalf("missing core.git: %q", out)
	}
	if !strings.Contains(out, "Destructive:") {
		t.Fatalf("missing Destructive category line: %q", out)
	}
}

func TestListPacksJSON(t *testing.T) {
	out, _, err := execCmd(t, "list", "packs", "--json")
	if err != nil {
		t.Fatalf("list packs --json error: %v", err)
	}
	var packs []map[string]any
	if err := json.Unmarshal([]byte(out), &packs); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(packs) == 0 {
		t.Fatal("expected non-empty packs list")
	}
	first := packs[0]
	for _, key := range []string{"Destructive", "Privacy", "Both"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("missing %s in JSON output", key)
		}
	}
}

func TestListRulesHuman(t *testing.T) {
	out, _, err := execCmd(t, "list", "rules")
	if err != nil {
		t.Fatalf("list rules error: %v", err)
	}
	if !strings.Contains(out, "Rules (") {
		t.Fatalf("missing Rules header: %q", out)
	}
	if !strings.Contains(out, "[Destructive:") {
		t.Fatalf("missing Destructive tag: %q", out)
	}
	if !strings.Contains(out, "[Privacy:") {
		t.Fatalf("missing Privacy tag: %q", out)
	}
	if !strings.Contains(out, "core.git") {
		t.Fatalf("missing core.git: %q", out)
	}
}

func TestListRulesJSON(t *testing.T) {
	out, _, err := execCmd(t, "list", "rules", "--json")
	if err != nil {
		t.Fatalf("list rules --json error: %v", err)
	}
	var rules []map[string]any
	if err := json.Unmarshal([]byte(out), &rules); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected non-empty rules list")
	}
	first := rules[0]
	for _, field := range []string{"ID", "PackID", "Category", "Severity", "Reason"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("missing %s in JSON output", field)
		}
	}
}
