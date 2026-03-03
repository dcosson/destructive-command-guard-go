package parse

import (
	"context"
	"testing"
)

// E3: Inline Script Detection Examples
// Tests that inline script patterns (python -c, bash -c, ruby -e, etc.)
// are properly handled. Currently tests surface-level extraction only;
// when InlineDetector is implemented, nested command extraction should be
// added to these checks.

type inlineTestCase struct {
	name           string
	input          string
	expectCommands []string // Expected surface-level command names
}

var inlineTests = []inlineTestCase{
	{
		name:           "python -c with os.system",
		input:          `python -c "import os; os.system('rm -rf /')"`,
		expectCommands: []string{"python"},
	},
	{
		name:           "bash -c simple",
		input:          `bash -c "rm -rf /tmp/foo"`,
		expectCommands: []string{"bash"},
	},
	{
		name:           "ruby -e with system",
		input:          `ruby -e "system('git push --force')"`,
		expectCommands: []string{"ruby"},
	},
	{
		name:           "node -e with execSync",
		input:          `node -e "require('child_process').execSync('rm -rf /')"`,
		expectCommands: []string{"node"},
	},
	{
		name:           "perl -e with system",
		input:          `perl -e 'system("rm -rf /")'`,
		expectCommands: []string{"perl"},
	},
	{
		name:           "eval simple",
		input:          `eval "rm -rf /"`,
		expectCommands: []string{"eval"},
	},
	{
		name:           "eval unquoted args",
		input:          `eval rm -rf /`,
		expectCommands: []string{"eval"},
	},
	// Negative cases — commands with -c/-e flags that are NOT inline triggers
	{
		name:           "gcc -c is not inline",
		input:          "gcc -c file.c",
		expectCommands: []string{"gcc"},
	},
	{
		name:           "tar -czf is not inline",
		input:          "tar -czf archive.tar.gz /data",
		expectCommands: []string{"tar"},
	},
	{
		name:           "python without -c is not inline",
		input:          "python script.py",
		expectCommands: []string{"python"},
	},
	{
		name:           "python -c with no arg",
		input:          "python -c",
		expectCommands: []string{"python"},
	},
	{
		name:           "ruby -e empty string",
		input:          `ruby -e ''`,
		expectCommands: []string{"ruby"},
	},
	{
		name:           "bash -c with variable ref",
		input:          `bash -c "$CMD"`,
		expectCommands: []string{"bash"},
	},
	// Inline with compound commands
	{
		name:           "bash -c in pipeline",
		input:          `echo hello | bash -c "cat"`,
		expectCommands: []string{"echo", "bash"},
	},
	{
		name:           "bash -c in and chain",
		input:          `test -f script.sh && bash -c "echo ok"`,
		expectCommands: []string{"test", "bash"},
	},
	// Inline with env prefix
	{
		name:           "env prefix with bash -c",
		input:          `LANG=C bash -c "echo hello"`,
		expectCommands: []string{"bash"},
	},
	{
		name:           "env command with bash -c",
		input:          `env LANG=C bash -c "echo hello"`,
		expectCommands: []string{"bash"},
	},
	// Heredoc patterns
	{
		name:           "heredoc to bash",
		input:          "bash <<'EOF'\nrm -rf /\nEOF",
		expectCommands: []string{"bash"},
	},
}

func TestInlineScriptDetection(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, tc := range inlineTests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := bp.ParseAndExtract(context.Background(), tc.input, 0)

			// Verify surface-level commands match expected
			gotNames := commandNames(result.Commands)
			if len(gotNames) < len(tc.expectCommands) {
				t.Fatalf("expected at least %d commands %v, got %d: %v",
					len(tc.expectCommands), tc.expectCommands, len(gotNames), gotNames)
			}

			// Check that expected commands appear in order
			matchIdx := 0
			for _, name := range gotNames {
				if matchIdx < len(tc.expectCommands) && name == tc.expectCommands[matchIdx] {
					matchIdx++
				}
			}
			if matchIdx < len(tc.expectCommands) {
				t.Errorf("expected commands %v in order, got %v (matched %d/%d)",
					tc.expectCommands, gotNames, matchIdx, len(tc.expectCommands))
			}
		})
	}
}
