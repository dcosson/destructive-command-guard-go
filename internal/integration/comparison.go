package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// ComparisonEntry represents a single comparison test case.
type ComparisonEntry struct {
	Command        string `json:"command"`
	GoDecision     string `json:"go_decision,omitempty"`
	GoSeverity     string `json:"go_severity,omitempty"`
	GoPack         string `json:"go_pack,omitempty"`
	RustDecision   string `json:"rust_decision,omitempty"`
	RustSeverity   string `json:"rust_severity,omitempty"`
	RustPack       string `json:"rust_pack,omitempty"`
	Classification string `json:"classification,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

// UpstreamResult is the parsed result from the upstream binary.
type UpstreamResult struct {
	Decision string `json:"decision"`
	Severity string `json:"severity,omitempty"`
	Pack     string `json:"pack,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

type KnownDivergence struct {
	Command        string `json:"command"`
	Classification string `json:"classification"`
	Rationale      string `json:"rationale"`
}

func loadComparisonCorpus(path string) ([]ComparisonEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []ComparisonEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func writeComparisonReport(path string, results []ComparisonEntry) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadKnownDivergences(path string) (map[string]KnownDivergence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []KnownDivergence
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	out := make(map[string]KnownDivergence, len(list))
	for _, d := range list {
		out[d.Command] = d
	}
	return out, nil
}

func classifyDivergence(entry ComparisonEntry, known map[string]KnownDivergence) string {
	if k, ok := known[entry.Command]; ok {
		return k.Classification
	}
	goSev := parseSeverity(entry.GoSeverity)
	rustSev := parseSeverity(entry.RustSeverity)

	if entry.GoDecision == "Deny" && entry.RustDecision == "Allow" {
		return "intentional_divergence"
	}
	if entry.GoDecision == "Allow" && entry.RustDecision == "Deny" {
		return "bug"
	}
	if entry.GoDecision == entry.RustDecision && abs(goSev-rustSev) <= 1 {
		return "intentional_divergence"
	}
	if entry.GoDecision == entry.RustDecision && abs(goSev-rustSev) >= 2 {
		return "bug"
	}
	return "bug"
}

func parseSeverity(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "indeterminate":
		return 0
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		// numeric fallback
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return n
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func severityString(result guard.Result) string {
	if result.DestructiveAssessment == nil {
		return ""
	}
	return result.DestructiveAssessment.Severity.String()
}

func runUpstream(binary, command string) (UpstreamResult, error) {
	cmd := exec.Command(binary, "check", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return parseUpstreamOutput(stdout.Bytes(), err, stderr.Bytes())
}

func parseUpstreamOutput(stdout []byte, runErr error, stderr []byte) (UpstreamResult, error) {
	out := strings.TrimSpace(string(stdout))
	if runErr != nil && out == "" {
		return UpstreamResult{}, fmt.Errorf("upstream run failed: %w: %s", runErr, strings.TrimSpace(string(stderr)))
	}
	if out == "" {
		return UpstreamResult{}, errors.New("empty upstream output")
	}

	var j struct {
		Decision string `json:"decision"`
		Severity string `json:"severity"`
		Pack     string `json:"pack"`
	}
	if err := json.Unmarshal(stdout, &j); err == nil && j.Decision != "" {
		return UpstreamResult{
			Decision: normalizeDecision(j.Decision),
			Severity: j.Severity,
			Pack:     j.Pack,
			Raw:      out,
		}, nil
	}

	// Text fallback: look for decision keyword.
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "deny"):
		return UpstreamResult{Decision: "Deny", Raw: out}, nil
	case strings.Contains(lower, "ask"):
		return UpstreamResult{Decision: "Ask", Raw: out}, nil
	case strings.Contains(lower, "allow"):
		return UpstreamResult{Decision: "Allow", Raw: out}, nil
	default:
		return UpstreamResult{}, fmt.Errorf("unrecognized upstream output: %s", out)
	}
}

func normalizeDecision(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "deny":
		return "Deny"
	case "ask":
		return "Ask"
	default:
		return "Allow"
	}
}
