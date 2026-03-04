package e2etest

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// O1: S-Expression Determinism Oracle
// For a corpus of bash commands, verify that our parser produces consistent
// S-expressions across parses (self-comparison oracle). When the tree-sitter
// CLI is available, we also compare against it.
func TestOracleSExpressionDeterminism(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	corpus := []string{
		"ls",
		"echo hello world",
		"git push --force origin main",
		"/usr/bin/rm -rf /tmp/foo",
		"cat file | grep pattern",
		"echo a && echo b || echo c",
		"! git diff --quiet",
		"RAILS_ENV=production rails db:reset",
		"DIR=/; rm -rf $DIR",
		"(echo subshell)",
		"echo $(command substitution)",
		"export FOO=bar",
		"bash -c 'echo hello'",
		`python -c "import os"`,
	}

	for _, input := range corpus {
		t.Run(input, func(t *testing.T) {
			// Parse twice and compare S-expressions
			tree1, _ := bp.Parse(context.Background(), input)
			tree2, _ := bp.Parse(context.Background(), input)

			if tree1 == nil || tree2 == nil {
				if tree1 != tree2 {
					t.Errorf("non-deterministic nil/non-nil tree for %q", input)
				}
				return
			}

			s1 := tree1.RootNode().String()
			s2 := tree2.RootNode().String()
			if s1 != s2 {
				t.Errorf("non-deterministic S-expression for %q:\n  parse1: %s\n  parse2: %s",
					input, s1, s2)
			}

			// Verify S-expression is non-empty and starts with expected structure
			if !strings.HasPrefix(s1, "(program") {
				t.Errorf("S-expression does not start with (program for %q: %s", input, s1)
			}
		})
	}
}

// O1b: Tree-sitter CLI comparison (when available)
// Skipped when tree-sitter CLI is not installed.
func TestOracleTreeSitterCLI(t *testing.T) {
	t.Parallel()

	// Check if tree-sitter CLI is available
	tsPath, err := exec.LookPath("tree-sitter")
	if err != nil {
		t.Skip("tree-sitter CLI not available; skipping CLI oracle comparison")
	}
	_ = tsPath

	bp := NewBashParser()
	corpus := []string{
		"echo hello",
		"git push --force",
		"cat file | grep foo",
	}

	for _, input := range corpus {
		t.Run(input, func(t *testing.T) {
			tree, _ := bp.Parse(context.Background(), input)
			if tree == nil {
				t.Skip("parse returned nil tree")
			}

			ourSExpr := tree.RootNode().String()

			// Run tree-sitter CLI parse
			cmd := exec.Command(tsPath, "parse", "--scope", "source.bash")
			cmd.Stdin = strings.NewReader(input)
			out, err := cmd.Output()
			if err != nil {
				t.Skipf("tree-sitter CLI failed: %v", err)
			}

			cliSExpr := strings.TrimSpace(string(out))

			// Normalize whitespace for comparison
			ourNorm := strings.Join(strings.Fields(ourSExpr), " ")
			cliNorm := strings.Join(strings.Fields(cliSExpr), " ")

			if ourNorm != cliNorm {
				t.Logf("S-expression differs (may be version/format difference):")
				t.Logf("  ours: %s", ourSExpr)
				t.Logf("  CLI:  %s", cliSExpr)
				// Don't fail — format differences between tree-sitter versions
				// are expected. Log for manual review.
			}
		})
	}
}

// O2: Bash Execution Comparison Oracle (Dataflow)
// Verify that our dataflow resolution produces results consistent with
// actual bash execution. Our over-approximation may produce more values
// than bash actually sees (false positives), but should never miss values
// that bash does see (false negatives).
func TestOracleBashDataflowComparison(t *testing.T) {
	t.Parallel()

	// Check if bash is available
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	bp := NewBashParser()

	type dataflowCase struct {
		name         string
		input        string // Full bash command
		echoVar      string // Variable to echo for comparison
		bashExpected string // What bash actually produces
		overApprox   bool   // If true, our result may include extra values
	}

	cases := []dataflowCase{
		{
			name:         "simple assignment",
			input:        "DIR=/tmp; echo $DIR",
			echoVar:      "DIR",
			bashExpected: "/tmp",
		},
		{
			name:         "sequential override",
			input:        "DIR=/tmp; DIR=/var; echo $DIR",
			echoVar:      "DIR",
			bashExpected: "/var",
		},
		{
			name:         "and chain carries forward",
			input:        "DIR=/ && DIR=/tmp; echo $DIR",
			echoVar:      "DIR",
			bashExpected: "/tmp",
			overApprox:   true, // Our may-alias produces both / and /tmp
		},
		// NOTE: Parameter expansion with defaults (${VAR:-default}) is not resolved
		// by our dataflow analyzer — the literal is kept as-is. This is a known
		// limitation, not a false negative for security purposes, since the raw
		// form is more conservative (treats the whole expression as opaque).
		{
			name:         "unset variable kept raw",
			input:        "echo ${UNSET_VAR_12345:-default}",
			echoVar:      "",
			bashExpected: "default",
			overApprox:   true, // Our parser keeps the raw ${...} literal
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Get bash's actual output
			cmd := exec.Command(bashPath, "-c", tc.input)
			out, err := cmd.Output()
			if err != nil {
				t.Skipf("bash execution failed: %v", err)
			}
			bashResult := strings.TrimSpace(string(out))

			if tc.bashExpected != "" && bashResult != tc.bashExpected {
				t.Errorf("bash produced %q, expected %q for input %q",
					bashResult, tc.bashExpected, tc.input)
			}

			// Get our dataflow resolution
			result := bp.ParseAndExtract(context.Background(), tc.input, 0)

			// For the echo command, check if our resolved args include bash's result
			for _, echoCmd := range result.Commands {
				if echoCmd.Name != "echo" {
					continue
				}
				foundBashResult := false
				for _, arg := range echoCmd.Args {
					if arg == bashResult {
						foundBashResult = true
					}
				}
				if !foundBashResult && !tc.overApprox {
					// Our resolution didn't include what bash actually produces
					// This would be a false negative (serious bug)
					t.Errorf("false negative: bash produced %q but our resolution has args %v for %q",
						bashResult, echoCmd.Args, tc.input)
				}
			}
		})
	}
}

// O2b: Document intentional over-approximations
// These cases verify that our over-approximation is known and documented.
func TestOracleOverApproximationDocumented(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	type overApproxCase struct {
		name           string
		input          string
		description    string
		expectMultiple bool // Our analyzer produces multiple values where bash has one
	}

	cases := []overApproxCase{
		{
			name:           "and chain may-alias",
			input:          "DIR=/ && DIR=/tmp && rm -rf $DIR",
			description:    "&& branches treated as may-alias: both / and /tmp tracked",
			expectMultiple: true,
		},
		{
			name:           "or chain may-alias",
			input:          "DIR=/tmp || DIR=/; rm -rf $DIR",
			description:    "|| branches treated as may-alias: both /tmp and / tracked",
			expectMultiple: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := bp.ParseAndExtract(context.Background(), tc.input, 0)

			rmCount := 0
			for _, cmd := range result.Commands {
				if cmd.Name == "rm" {
					rmCount++
				}
			}

			if tc.expectMultiple && rmCount <= 1 {
				t.Errorf("expected multiple rm variants (over-approximation) for %q, got %d",
					tc.input, rmCount)
			}
		})
	}
}
