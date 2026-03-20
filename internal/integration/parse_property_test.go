package integration

import (
	"context"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// P1: Parse Never Panics
// For any []byte input of any length, BashParser.Parse() returns without panic.
func TestPropertyParseNeverPanics(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	f := func(input []byte) bool {
		panicked := false
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		bp.Parse(context.Background(), string(input))
		return !panicked
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil {
		t.Fatal(err)
	}
}

// P1b: Parse Never Panics with bash-like inputs (higher signal than random bytes).
func TestPropertyParseNeverPanicsStructured(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	r := rand.New(rand.NewSource(42))
	for i := 0; i < 5000; i++ {
		input := generateBashLikeInput(r)
		assertNoPanic(t, "Parse(bash-like)", func() {
			bp.Parse(context.Background(), input)
		})
	}
}

// P1c: Parse Never Panics with adversarial inputs.
func TestPropertyParseNeverPanicsAdversarial(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, input := range adversarialInputs {
		assertNoPanic(t, "Parse(adversarial)", func() {
			bp.Parse(context.Background(), input)
		})
	}
}

// P1d: Parse Never Panics with real-world command corpus.
func TestPropertyParseNeverPanicsRealWorld(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, input := range realWorldCommands {
		assertNoPanic(t, "Parse(real-world)", func() {
			bp.Parse(context.Background(), input)
		})
	}
}

// P1e: Parse returns consistent results — parsing the same input twice yields
// equivalent tree structure (determinism check).
func TestPropertyParseDeterministic(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, input := range realWorldCommands {
		if input == "" || strings.TrimSpace(input) == "" {
			continue
		}
		tree1, w1 := bp.Parse(context.Background(), input)
		tree2, w2 := bp.Parse(context.Background(), input)

		if (tree1 == nil) != (tree2 == nil) {
			t.Errorf("non-deterministic nil/non-nil tree for %q", input)
		}
		if len(w1) != len(w2) {
			t.Errorf("non-deterministic warning count for %q: %d vs %d", input, len(w1), len(w2))
		}
		if tree1 != nil && tree2 != nil {
			s1 := tree1.RootNode().String()
			s2 := tree2.RootNode().String()
			if s1 != s2 {
				t.Errorf("non-deterministic S-expression for %q:\n  %s\n  %s", input, s1, s2)
			}
		}
	}
}

// P1f: Parse valid commands always returns a non-nil tree.
func TestPropertyValidCommandsReturnTree(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	validCommands := []string{
		"ls",
		"echo hello",
		"git push --force",
		"rm -rf /tmp/foo",
		"cat file | grep pattern",
		"echo a && echo b",
		"echo a || echo b",
		"(echo subshell)",
		"FOO=bar echo $FOO",
	}

	for _, input := range validCommands {
		tree, warnings := bp.Parse(context.Background(), input)
		if tree == nil {
			t.Errorf("expected non-nil tree for valid input %q, warnings: %v", input, warnings)
		}
	}
}

// P1g: Parse input at exactly MaxInputSize boundary.
func TestPropertyMaxInputSizeBoundary(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Exactly at limit — should parse (use single-token string to avoid slow deep AST)
	atLimit := strings.Repeat("a", MaxInputSize)
	tree, warnings := bp.Parse(context.Background(), atLimit)
	if tree == nil {
		t.Error("expected non-nil tree at MaxInputSize boundary")
	}
	if hasWarning(warnings, WarnInputTruncated) {
		t.Error("did not expect WarnInputTruncated at boundary")
	}

	// One byte over — should reject
	overLimit := strings.Repeat("a", MaxInputSize+1)
	tree, warnings = bp.Parse(context.Background(), overLimit)
	if tree != nil {
		t.Error("expected nil tree above MaxInputSize")
	}
	if !hasWarning(warnings, WarnInputTruncated) {
		t.Error("expected WarnInputTruncated above boundary")
	}
}

// P1h: Parse tree root is always "program" for valid non-empty inputs.
func TestPropertyParseRootNodeType(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	for _, input := range realWorldCommands {
		if input == "" || strings.TrimSpace(input) == "" {
			continue
		}
		tree, _ := bp.Parse(context.Background(), input)
		if tree == nil {
			continue
		}
		rootType := tree.RootNode().Type()
		if rootType != "program" {
			t.Errorf("expected root node type 'program', got %q for input %q", rootType, input)
		}
	}
}

// truncate returns first n bytes of input for logging.
func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
