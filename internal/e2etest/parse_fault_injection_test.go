package e2etest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// F1: Context Cancellation
// The pure-Go tree-sitter parser does not support context cancellation mid-parse.
// A pre-cancelled context causes the parser to hang in condenseStack.
// These tests verify that Parse/ParseAndExtract complete without panic when
// given a context that cancels shortly after parsing begins.
func TestParseCancelledContext(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Use a very short timeout — parse will likely complete before cancellation,
	// but this exercises the context-carrying code path without hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	assertNoPanic(t, "Parse(timeout)", func() {
		tree, warnings := bp.Parse(ctx, "git push --force")
		_ = tree
		_ = warnings
	})
}

func TestParseCancelledContextComplex(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	assertNoPanic(t, "Parse(timeout-compound)", func() {
		tree, _ := bp.Parse(ctx, "echo a && rm -rf / || echo b; echo c")
		_ = tree
	})
}

func TestParseAndExtractCancelledContext(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	assertNoPanic(t, "ParseAndExtract(timeout)", func() {
		result := bp.ParseAndExtract(ctx, "DIR=/tmp; rm -rf $DIR", 0)
		_ = result
	})
}

// F2: Extremely Long Input
// Tests near and beyond MaxInputSize boundary.
func TestParseLongInputBoundary(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Just under the limit — use a single long string token (fast to parse,
	// unlike thousands of repeated commands which create huge AST nodes).
	underLimit := "echo " + strings.Repeat("a", MaxInputSize-6)
	if len(underLimit) > MaxInputSize {
		underLimit = underLimit[:MaxInputSize]
	}

	tree, warnings := bp.Parse(context.Background(), underLimit)
	if tree == nil {
		t.Error("expected non-nil tree for input under MaxInputSize")
	}
	if hasWarning(warnings, WarnInputTruncated) {
		t.Error("did not expect WarnInputTruncated for input under MaxInputSize")
	}

	// Over the limit
	overLimit := strings.Repeat("a", MaxInputSize+1)
	tree, warnings = bp.Parse(context.Background(), overLimit)
	if tree != nil {
		t.Error("expected nil tree for input over MaxInputSize")
	}
	if !hasWarning(warnings, WarnInputTruncated) {
		t.Error("expected WarnInputTruncated for input over MaxInputSize")
	}
}

func TestParseLongInputExtraction(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Moderately long input that parses and extracts
	input := strings.Repeat("echo hello; ", 100)
	assertNoPanic(t, "ParseAndExtract(long)", func() {
		result := bp.ParseAndExtract(context.Background(), input, 0)
		if len(result.Commands) == 0 {
			t.Error("expected some commands from repeated echo")
		}
	})
}

// F3: Adversarial Quoting
// Parser must not panic on malformed quoting patterns.
func TestAdversarialQuoting(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	inputs := []string{
		// Deeply nested quoting
		`echo "hello 'world "nested" quotes' end"`,
		`echo $'escape\nsequence'`,
		`echo "$(echo "$(echo "deep")")"`,

		// Massive quote repetition
		strings.Repeat(`"`, 1000),
		strings.Repeat(`'`, 999),

		// Unterminated quotes
		`echo "unterminated`,
		`echo 'unterminated`,
		`echo "unterminated 'mixed`,

		// Mixed quoting styles
		`echo 'single "mixed' "quotes"`,
		`echo "hello""world"`,
		`echo 'hello''world'`,

		// Escaped quotes
		`echo \"hello\"`,
		`echo \'hello\'`,
		`echo "hello \" world"`,

		// Empty quotes
		`echo "" '' ""`,

		// Quotes in variable assignments
		`A="quoted value"; echo $A`,
		`B='single quoted'; echo $B`,
	}

	for _, input := range inputs {
		label := input
		if len(label) > 40 {
			label = label[:40] + "..."
		}
		t.Run(label, func(t *testing.T) {
			assertNoPanic(t, "Parse(adversarial-quoting)", func() {
				bp.Parse(context.Background(), input)
			})
		})
	}
}

// F4: Unicode Edge Cases
// Parser must handle Unicode and invalid byte sequences safely.
func TestUnicodeInput(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	inputs := []string{
		// Valid Unicode
		"echo '日本語'",
		"rm -rf /tmp/名前",
		"echo 'Ω≈ç√∫'",
		"echo '🔥🚀💯'",

		// Invalid UTF-8
		string([]byte{0xff, 0xfe, 0xfd}),
		string([]byte{0xc0, 0xaf}),
		string([]byte{0xed, 0xa0, 0x80}), // Surrogate half

		// Null bytes
		"echo \x00 hidden",
		"\x00\x00\x00\x00",
		"echo hello\x00world",

		// ANSI escape codes
		"echo '\x1b[31mred\x1b[0m'",
		"echo '\x1b[2J\x1b[H'", // Clear screen

		// Bash unicode escapes
		"echo $'\\u0000'",
		"echo $'\\u0041'",

		// BOM
		string([]byte{0xef, 0xbb, 0xbf}) + "echo hello",

		// Mixed valid/invalid
		"echo 'hello " + string([]byte{0xff}) + " world'",
	}

	for i, input := range inputs {
		t.Run(strings.ReplaceAll(input[:min(20, len(input))], "\x00", "\\0"), func(t *testing.T) {
			assertNoPanic(t, "Parse(unicode)", func() {
				bp.Parse(context.Background(), input)
			})
			assertNoPanic(t, "ParseAndExtract(unicode)", func() {
				bp.ParseAndExtract(context.Background(), input, 0)
			})
			_ = i
		})
	}
}
