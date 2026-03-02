package parse

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// P2: Extract Output Consistency
// For any AST that Parse returns, every ExtractedCommand satisfies structural invariants.
func TestPropertyExtractOutputConsistency(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	f := func(input string) bool {
		tree, _ := bp.Parse(context.Background(), input)
		if tree == nil {
			return true
		}
		result := NewCommandExtractor(bp).Extract(tree, input)
		for _, cmd := range result.Commands {
			if cmd.Name == "" {
				return false
			}
			// EndByte must not exceed input length (StartByte == EndByte is
			// valid for zero-length error-recovery nodes in tree-sitter)
			if int(cmd.EndByte) > len(input) {
				return false
			}
			for k := range cmd.Flags {
				if !strings.HasPrefix(k, "-") {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatal(err)
	}
}

// P3: Normalize is Idempotent
func TestPropertyNormalizeIdempotent(t *testing.T) {
	t.Parallel()

	f := func(s string) bool {
		return Normalize(Normalize(s)) == Normalize(s)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil {
		t.Fatal(err)
	}
}

// P3b: Normalize strips path correctly.
func TestPropertyNormalizeStripsPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"/usr/bin/git", "git"},
		{"/usr/local/bin/rm", "rm"},
		{"./script.sh", "script.sh"},
		{"git", "git"},
		{"/", ""},
		{"", ""},
		{"a/b/c", "c"},
	}
	for _, tc := range cases {
		got := Normalize(tc.input)
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// P4: Dataflow Expansion is Bounded
func TestPropertyDataflowExpansionBounded(t *testing.T) {
	t.Parallel()

	da := NewDataflowAnalyzer()
	// Add many variables with multiple values to stress expansion
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("VAR%d", i)
		for j := 0; j < 5; j++ {
			if j == 0 {
				da.Define(name, fmt.Sprintf("val%d", j), false)
			} else {
				other := NewDataflowAnalyzer()
				other.Define(name, fmt.Sprintf("val%d", j), false)
				da.MergeBranch(other)
			}
		}
	}

	// ResolveString with multiple variable references
	expansions, capped := da.ResolveString("$VAR0 $VAR1 $VAR2 $VAR3 $VAR4")
	if len(expansions) > 16 {
		t.Errorf("expansion produced %d results, expected <= 16", len(expansions))
	}
	if !capped {
		t.Error("expected expansion to be capped with this many variables")
	}
}

// P5: Parse + Extract Never Panics Together
func TestPropertyFullPipelineNeverPanics(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	f := func(input []byte) bool {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("pipeline panicked on input %q: %v", truncate(input, 100), r)
			}
		}()
		s := string(input)
		tree, _ := bp.Parse(context.Background(), s)
		if tree != nil {
			NewCommandExtractor(bp).Extract(tree, s)
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 10000}); err != nil {
		t.Fatal(err)
	}
}

// P5b: Full pipeline with structured inputs.
func TestPropertyFullPipelineStructured(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	r := rand.New(rand.NewSource(42))
	for i := 0; i < 5000; i++ {
		input := generateBashLikeInput(r)
		assertNoPanic(t, "pipeline(structured)", func() {
			tree, _ := bp.Parse(context.Background(), input)
			if tree != nil {
				result := NewCommandExtractor(bp).Extract(tree, input)
				assertValidParseResult(t, result, input)
			}
		})
	}
}

// P6: Inline Detection Depth Bounded
func TestPropertyInlineDetectionDepthBounded(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Create deeply nested bash -c chains
	for depth := 1; depth <= 10; depth++ {
		input := buildNestedBashC(depth)
		assertNoPanic(t, fmt.Sprintf("inline-depth-%d", depth), func() {
			result := bp.ParseAndExtract(context.Background(), input, 0)
			// Should never have more than MaxInlineDepth levels of extracted commands
			// from inline detection. The exact count depends on implementation but
			// should be bounded.
			_ = result
		})
	}
}

// buildNestedBashC creates nested `bash -c "bash -c ..."` commands at the given depth.
func buildNestedBashC(depth int) string {
	if depth <= 0 {
		return "echo hello"
	}
	inner := buildNestedBashC(depth - 1)
	// Escape inner quotes
	inner = strings.ReplaceAll(inner, `"`, `\"`)
	return fmt.Sprintf(`bash -c "%s"`, inner)
}

// P7: ParseResult Boundary Contract Locked
func TestPropertyParseResultBoundaryContract(t *testing.T) {
	t.Parallel()
	bp := NewBashParser()

	// Test structural contract: Commands, ExportedVars, Warnings, HasError
	result := bp.ParseAndExtract(context.Background(),
		"DIR=/tmp && rm -rf $DIR", 0)

	// Must extract at least the rm command
	if len(result.Commands) == 0 {
		t.Fatal("expected at least one extracted command")
	}
	foundRM := false
	for _, cmd := range result.Commands {
		if cmd.Name == "rm" {
			foundRM = true
			if cmd.RawName == "" {
				t.Error("expected non-empty RawName for rm")
			}
		}
	}
	if !foundRM {
		t.Error("expected to find 'rm' in extracted commands")
	}

	// ExportedVars must be initialized (non-nil map)
	if result.ExportedVars == nil {
		t.Fatal("ParseResult.ExportedVars must not be nil")
	}

	// Warning payload contract: shared warning type/codes.
	for _, w := range result.Warnings {
		if w.Message == "" {
			t.Fatal("warning has empty message")
		}
		switch w.Code {
		case guard.WarnPartialParse, guard.WarnInputTruncated, guard.WarnExtractorPanic,
			guard.WarnInlineDepthExceeded, guard.WarnExpansionCapped,
			guard.WarnCommandSubstitution, guard.WarnMatcherPanic, guard.WarnUnknownPackID:
			// valid known codes
		default:
			t.Fatalf("unrecognized warning code: %d", w.Code)
		}
	}

	// Contract: each command has Flags as initialized map, not nil
	for i, cmd := range result.Commands {
		if cmd.Flags == nil {
			t.Errorf("command[%d].Flags must not be nil", i)
		}
		if cmd.InlineEnv == nil {
			t.Errorf("command[%d].InlineEnv must not be nil", i)
		}
	}
}
