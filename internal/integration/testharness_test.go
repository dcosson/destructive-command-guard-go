package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHasRegisteredPack(t *testing.T) {
	if !HasRegisteredPack("core.git") {
		t.Fatal("expected core.git pack to be registered")
	}
	if HasRegisteredPack("does.not.exist") {
		t.Fatal("unexpected pack registration")
	}
}

func TestFindModuleRoot(t *testing.T) {
	root, err := FindModuleRoot()
	if err != nil {
		t.Fatalf("FindModuleRoot error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod missing at root %s: %v", root, err)
	}
}

func TestWriteBenchResults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bench", "results.json")
	in := []BenchResult{
		{Name: "a", NsPerOp: 10.5, AllocsPerOp: 1, BytesPerOp: 64},
		{Name: "b", NsPerOp: 20.0, AllocsPerOp: 2, BytesPerOp: 128},
	}
	if err := WriteBenchResults(path, in); err != nil {
		t.Fatalf("WriteBenchResults error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var out []BenchResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("results len = %d, want %d", len(out), len(in))
	}
}
