package e2etest

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// BenchResult is a stable JSON shape for CI benchmark trend tracking.
type BenchResult struct {
	Name        string  `json:"name"`
	NsPerOp     float64 `json:"ns_per_op"`
	AllocsPerOp int64   `json:"allocs_per_op"`
	BytesPerOp  int64   `json:"bytes_per_op"`
}

// WriteBenchResults writes benchmark results to path as JSON.
func WriteBenchResults(path string, results []BenchResult) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
