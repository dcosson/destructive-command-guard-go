package parse

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestBashParserSimpleCommand(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	tree, warnings := parser.Parse(context.Background(), "echo hello")

	if tree == nil {
		t.Fatalf("expected non-nil tree")
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if tree.RootNode().Type() == "" {
		t.Fatalf("expected non-empty root node type")
	}
}

func TestBashParserCompoundCommand(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	tree, warnings := parser.Parse(context.Background(), "echo hello && rm -rf /tmp/a")

	if tree == nil {
		t.Fatalf("expected non-nil tree")
	}
	if hasWarningCode(warnings, WarnInputTruncated) {
		t.Fatalf("unexpected input size warning: %v", warnings)
	}
	if tree.RootNode().ChildCount() == 0 {
		t.Fatalf("expected root node to have children")
	}
}

func TestBashParserMalformedCommand(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	tree, warnings := parser.Parse(context.Background(), "echo \"unterminated")

	if tree == nil {
		t.Fatalf("expected parser recovery tree for malformed input")
	}
	if hasWarningCode(warnings, WarnInputTruncated) {
		t.Fatalf("unexpected size warning for malformed input: %v", warnings)
	}
	if hasWarningCode(warnings, WarnExtractorPanic) {
		t.Fatalf("unexpected panic warning for malformed input: %v", warnings)
	}
}

func TestBashParserMaxInputBoundary(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()

	atLimit := strings.Repeat("a", MaxInputSize)
	tree, warnings := parser.Parse(context.Background(), atLimit)
	if tree == nil {
		t.Fatalf("expected parse tree at max boundary")
	}
	if hasWarningCode(warnings, WarnInputTruncated) {
		t.Fatalf("did not expect size warning at boundary")
	}

	overLimit := strings.Repeat("a", MaxInputSize+1)
	tree, warnings = parser.Parse(context.Background(), overLimit)
	if tree != nil {
		t.Fatalf("expected nil tree above max boundary")
	}
	if !hasWarningCode(warnings, WarnInputTruncated) {
		t.Fatalf("expected size warning above boundary, got %v", warnings)
	}
}

func TestBashParserConcurrentStress(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	inputs := []string{
		"echo hello",
		"git status && git add .",
		"cat /tmp/a | grep foo",
		"VAR=1 env | grep VAR",
		"if true; then echo ok; fi",
	}

	const workers = 24
	const iterations = 100

	var wg sync.WaitGroup
	errCh := make(chan string, workers*iterations)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				input := inputs[(workerID+j)%len(inputs)]
				tree, warnings := parser.Parse(context.Background(), input)
				if tree == nil {
					errCh <- "nil tree for valid input"
					return
				}
				if hasWarningCode(warnings, WarnExtractorPanic) {
					errCh <- "panic warning observed"
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrency stress failure: %s", err)
	}
}

func hasWarningCode(warnings []Warning, code WarningCode) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}
