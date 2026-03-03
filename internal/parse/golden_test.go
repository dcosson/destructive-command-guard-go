package parse

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files with actual output")

// goldenEntry represents a single input→output pair in a golden file.
type goldenEntry struct {
	Input  string
	Output string // JSON-encoded extraction result
}

// parseGoldenFile reads a golden file in the format:
//
//	---INPUT---
//	<command>
//	---OUTPUT---
//	<JSON>
//	---INPUT---
//	...
func parseGoldenFile(t *testing.T, path string) []goldenEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", path, err)
	}

	var entries []goldenEntry
	sections := strings.Split(string(data), "---INPUT---\n")
	for _, section := range sections {
		// Don't TrimSpace the whole section — the input might be empty or whitespace
		if section == "" {
			continue
		}
		parts := strings.SplitN(section, "---OUTPUT---\n", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed golden entry in %s: %q", path, section)
		}
		// Input: strip trailing newline (the one before ---OUTPUT---)
		input := strings.TrimRight(parts[0], "\n")
		entries = append(entries, goldenEntry{
			Input:  input,
			Output: strings.TrimRight(parts[1], "\n"),
		})
	}
	return entries
}

// goldenResult is the subset of ParseResult we serialize for golden comparison.
type goldenResult struct {
	Commands []goldenCommand `json:"commands"`
	HasError bool            `json:"has_error,omitempty"`
}

type goldenCommand struct {
	Name             string            `json:"name"`
	RawName          string            `json:"raw_name"`
	Args             []string          `json:"args,omitempty"`
	Flags            map[string]string `json:"flags,omitempty"`
	InlineEnv        map[string]string `json:"inline_env,omitempty"`
	InPipeline       bool              `json:"in_pipeline,omitempty"`
	Negated          bool              `json:"negated,omitempty"`
	DataflowResolved bool              `json:"dataflow_resolved,omitempty"`
}

func toGoldenResult(result ParseResult) goldenResult {
	gr := goldenResult{HasError: result.HasError}
	for _, cmd := range result.Commands {
		gc := goldenCommand{
			Name:             cmd.Name,
			RawName:          cmd.RawName,
			Args:             cmd.Args,
			Flags:            cmd.Flags,
			InlineEnv:        cmd.InlineEnv,
			InPipeline:       cmd.InPipeline,
			Negated:          cmd.Negated,
			DataflowResolved: cmd.DataflowResolved,
		}
		if gc.Flags == nil {
			gc.Flags = map[string]string{}
		}
		if gc.InlineEnv == nil {
			gc.InlineEnv = map[string]string{}
		}
		gr.Commands = append(gr.Commands, gc)
	}
	if gr.Commands == nil {
		gr.Commands = []goldenCommand{}
	}
	return gr
}

func marshalGolden(result goldenResult) string {
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b)
}

func writeGoldenFile(t *testing.T, path string, entries []goldenEntry) {
	t.Helper()
	var buf strings.Builder
	for _, e := range entries {
		buf.WriteString("---INPUT---\n")
		buf.WriteString(e.Input)
		buf.WriteString("\n---OUTPUT---\n")
		buf.WriteString(e.Output)
		buf.WriteString("\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating golden dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("writing golden file: %v", err)
	}
}

// runGoldenTest is the core golden file test driver. It reads the golden file,
// runs extraction on each input, and compares/updates the output.
func runGoldenTest(t *testing.T, goldenPath string, inputs []string) {
	t.Helper()
	bp := NewBashParser()

	if *updateGolden {
		var entries []goldenEntry
		for _, input := range inputs {
			result := bp.ParseAndExtract(context.Background(), input, 0)
			gr := toGoldenResult(result)
			entries = append(entries, goldenEntry{
				Input:  input,
				Output: marshalGolden(gr),
			})
		}
		writeGoldenFile(t, goldenPath, entries)
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	entries := parseGoldenFile(t, goldenPath)
	if len(entries) != len(inputs) {
		t.Fatalf("golden file has %d entries but test has %d inputs (run with -update-golden to refresh)",
			len(entries), len(inputs))
	}

	for i, entry := range entries {
		if entry.Input != inputs[i] {
			t.Fatalf("golden entry %d input mismatch:\n  golden: %q\n  test:   %q\n(run with -update-golden to refresh)",
				i, entry.Input, inputs[i])
		}

		result := bp.ParseAndExtract(context.Background(), entry.Input, 0)
		actual := marshalGolden(toGoldenResult(result))

		if actual != entry.Output {
			t.Errorf("golden mismatch for input %q:\n--- expected ---\n%s\n--- actual ---\n%s\n(run with -update-golden to refresh)",
				entry.Input, entry.Output, actual)
		}
	}
}

// Golden test inputs — representative commands for regression testing.
// Each input is chosen so that the parser produces correct, expected output.
// Commands that trigger known parser quirks (e.g., single-dash long flags
// like -auto-approve being split into character flags) are NOT included here;
// those bugs are documented with BUG tags in example_extraction_test.go.
var goldenSimpleInputs = []string{
	"ls",
	"echo hello",
	"git push --force origin main",
	"/usr/bin/rm -rf /tmp/foo",
	"rm -rf /",
	"cp -r /tmp/src /tmp/dst",
	"docker system prune -af",
	"kubectl delete namespace production",
	"terraform destroy --auto-approve",
	"redis-cli FLUSHALL",
}

var goldenCompoundInputs = []string{
	"echo a && echo b",
	"test -d /tmp || mkdir /tmp",
	"echo a; echo b",
	"cat file | grep pattern",
	"! git diff --quiet",
	"cd /tmp && rm -rf *",
}

var goldenDataflowInputs = []string{
	"DIR=/; rm -rf $DIR",
	"DIR=/tmp || DIR=/; rm -rf $DIR",
	"DIR=/tmp; DIR=/; rm -rf $DIR",
	"RAILS_ENV=production rails db:reset",
	"FOO=bar BAZ=qux echo test",
	"env NODE_ENV=production node server.js",
}

var goldenErrorInputs = []string{
	"",
	"   ",
	`git push "`,
	"git push &&& rm -rf /",
	"|",
}

func TestGoldenSimpleCommands(t *testing.T) {
	t.Parallel()
	runGoldenTest(t, filepath.Join("testdata", "golden", "simple_commands.golden"), goldenSimpleInputs)
}

func TestGoldenCompoundCommands(t *testing.T) {
	t.Parallel()
	runGoldenTest(t, filepath.Join("testdata", "golden", "compound_commands.golden"), goldenCompoundInputs)
}

func TestGoldenDataflow(t *testing.T) {
	t.Parallel()
	runGoldenTest(t, filepath.Join("testdata", "golden", "dataflow.golden"), goldenDataflowInputs)
}

func TestGoldenErrorRecovery(t *testing.T) {
	t.Parallel()
	runGoldenTest(t, filepath.Join("testdata", "golden", "error_recovery.golden"), goldenErrorInputs)
}
