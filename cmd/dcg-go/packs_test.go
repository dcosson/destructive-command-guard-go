package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunPacksModeHuman(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runPacksMode(nil); err != nil {
		t.Fatalf("runPacksMode error: %v", err)
	}
	out := stdout.(*bytes.Buffer).String()
	if !strings.Contains(out, "Registered packs") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "core.git") {
		t.Fatalf("missing core.git: %q", out)
	}
}

func TestRunPacksModeJSON(t *testing.T) {
	reset := withIO(t)
	defer reset()

	if err := runPacksMode([]string{"--json"}); err != nil {
		t.Fatalf("runPacksMode error: %v", err)
	}
	var out []map[string]any
	if err := json.Unmarshal(stdout.(*bytes.Buffer).Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty packs list")
	}
}
