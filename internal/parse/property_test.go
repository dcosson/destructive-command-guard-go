package parse

import (
	"context"
	"strings"
	"testing"
	"testing/quick"
)

func TestPropertyParseNeverPanics(t *testing.T) {
	t.Parallel()

	p := NewBashParser()
	cfg := &quick.Config{MaxCount: 200}
	prop := func(in string) bool {
		panicked := false
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_, _ = p.Parse(context.Background(), in)
		return !panicked
	}
	if err := quick.Check(prop, cfg); err != nil {
		t.Fatalf("property failed: %v", err)
	}
}

func TestPropertyNormalizeIdempotent(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 500}
	prop := func(in string) bool {
		n1 := Normalize(in)
		n2 := Normalize(n1)
		return n1 == n2
	}
	if err := quick.Check(prop, cfg); err != nil {
		t.Fatalf("property failed: %v", err)
	}
}

func TestPropertyResolveStringBounded(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 200}
	prop := func(in string) bool {
		da := NewDataflowAnalyzer()
		// Seed many branching definitions to try to blow up combinations.
		for i := 0; i < 6; i++ {
			name := "V" + string(rune('A'+i))
			da.Define(name, "0", false)
			branch := NewDataflowAnalyzer()
			branch.Define(name, "1", false)
			da.MergeBranch(branch)
		}
		exp, _ := da.ResolveString(in + " $VA $VB $VC $VD $VE $VF")
		return len(exp) <= maxExpansions
	}
	if err := quick.Check(prop, cfg); err != nil {
		t.Fatalf("property failed: %v", err)
	}
}

func TestPropertyExtractRawTextBounds(t *testing.T) {
	t.Parallel()

	p := NewBashParser()
	cfg := &quick.Config{MaxCount: 200}
	prop := func(in string) bool {
		result := p.ParseAndExtract(context.Background(), in, 0)
		for _, cmd := range result.Commands {
			if cmd.EndByte < cmd.StartByte {
				return false
			}
			if int(cmd.EndByte) > len(in) {
				return false
			}
			start := int(cmd.StartByte)
			end := int(cmd.EndByte)
			if start < 0 || end < start || end > len(in) {
				return false
			}
			if cmd.RawText == "" && end > start {
				continue
			}
			expected := in[start:end]
			if cmd.RawText != "" && cmd.RawText != expected {
				return false
			}
			// Keep containment check as a weak fallback invariant.
			if cmd.RawText != "" && !strings.Contains(in, cmd.RawText) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, cfg); err != nil {
		t.Fatalf("property failed: %v", err)
	}
}
