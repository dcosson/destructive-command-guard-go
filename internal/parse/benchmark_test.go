package parse

import (
	"context"
	"strings"
	"testing"
)

func BenchmarkParse(b *testing.B) {
	input := strings.Repeat("echo hello && ", 32) + "rm -rf /tmp/x"
	p := NewBashParser()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(context.Background(), input)
	}
}

func BenchmarkExtract(b *testing.B) {
	input := strings.Repeat("A=/tmp || A=/ && ", 8) + "rm -rf $A"
	p := NewBashParser()
	tree, _ := p.Parse(context.Background(), input)
	ex := NewCommandExtractor(p)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ex.Extract(tree, input)
	}
}

func BenchmarkDataflowResolveString(b *testing.B) {
	da := NewDataflowAnalyzer()
	for i := 0; i < 6; i++ {
		name := "V" + string(rune('A'+i))
		da.Define(name, "one", false)
		branch := NewDataflowAnalyzer()
		branch.Define(name, "two", false)
		da.MergeBranch(branch)
	}
	input := "$VA $VB $VC $VD $VE $VF"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = da.ResolveString(input)
	}
}

func BenchmarkFullPipeline(b *testing.B) {
	input := `python -c "import os; os.system('rm -rf /tmp/x')" && DIR=/tmp || DIR=/; rm -rf $DIR`
	p := NewBashParser()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.ParseAndExtract(context.Background(), input, 0)
	}
}
