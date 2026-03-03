package parse

import (
	"context"
	"testing"
)

// E4: Error Recovery Examples
// Tests for partial parse behavior with malformed input. Verifies that
// the parser gracefully handles broken syntax and still extracts what it can.
// Tree-sitter recovers gracefully from many malformed inputs without flagging
// them as errors (HasError=false), which is intentional behavior.

type recoveryTestCase struct {
	name       string
	input      string
	expectSome bool // Should extract at least some commands?
	hasError   bool // Should ParseResult.HasError be true?
}

var recoveryTests = []recoveryTestCase{
	// Tree-sitter recovers gracefully from these — no error flag
	{"unmatched double quote", `git push "`, true, false},
	{"unmatched single quote", `echo 'unterminated`, true, false},
	{"dangling pipe", "echo hello |", true, false},
	{"unmatched paren", "(git push", true, false},
	{"broken redirect", "echo >", true, false},
	{"nested unmatched quotes", `echo "hello 'world`, true, false},

	// These trigger tree-sitter parse errors
	{"triple &&", "git push &&& rm -rf /", true, true},
	{"just pipe", "|", false, true},

	// Valid input — no errors
	{"valid input", "git push", true, false},

	// Empty/whitespace — no commands, no errors
	{"empty", "", false, false},
	{"whitespace only", "   ", false, false},
	{"just semicolons", ";;;", false, false},
	{"just ampersand", "&", false, false},
}

func TestErrorRecovery(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, tc := range recoveryTests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := bp.ParseAndExtract(context.Background(), tc.input, 0)

			if tc.expectSome && len(result.Commands) == 0 {
				t.Errorf("expected at least some commands for %q, got none", tc.input)
			}
			if !tc.expectSome && len(result.Commands) > 0 {
				t.Errorf("expected no commands for %q, got %d: %v",
					tc.input, len(result.Commands), commandNames(result.Commands))
			}

			if tc.hasError != result.HasError {
				t.Errorf("HasError = %v, want %v for input %q (warnings: %v)",
					result.HasError, tc.hasError, tc.input, result.Warnings)
			}
		})
	}
}
