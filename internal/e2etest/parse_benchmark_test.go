package e2etest

import (
	"context"
	"strings"
	"testing"

	"github.com/dcosson/treesitter-go/languages/bash"
	tsp "github.com/dcosson/treesitter-go/parser"
)

var benchmarkCommandCases = []struct {
	name  string
	input string
}{
	{name: "short", input: "git push --force"},
	{name: "medium", input: "RAILS_ENV=production rails db:reset && git push --force origin main"},
	{name: "long", input: strings.Repeat("cat /var/log/app.log | grep error | ", 4) + "sort | uniq -c"},
	{name: "very_long", input: strings.Repeat("echo hello && ", 250) + "rm -rf /tmp/x"},
	{name: "max_boundary", input: strings.Repeat("a", MaxInputSize)},
}

func BenchmarkParseLatency(b *testing.B) {
	p := NewBashParser()
	ctx := context.Background()
	for _, tc := range benchmarkCommandCases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = p.Parse(ctx, tc.input)
			}
		})
	}
}

func BenchmarkExtractLatency(b *testing.B) {
	p := NewBashParser()
	extractor := NewCommandExtractor(p)
	ctx := context.Background()

	for _, tc := range benchmarkCommandCases {
		tc := tc
		tree, _ := p.Parse(ctx, tc.input)
		if tree == nil {
			continue
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = extractor.Extract(tree, tc.input)
			}
		})
	}
}

func BenchmarkDataflowResolution(b *testing.B) {
	cases := []struct {
		name string
		fn   func() *DataflowAnalyzer
		in   string
	}{
		{
			name: "no_variables",
			fn: func() *DataflowAnalyzer {
				return NewDataflowAnalyzer()
			},
			in: "plain-string-no-vars",
		},
		{
			name: "one_var_one_value",
			fn: func() *DataflowAnalyzer {
				da := NewDataflowAnalyzer()
				da.Define("DIR", "/tmp", false)
				return da
			},
			in: "$DIR",
		},
		{
			name: "three_vars_one_each",
			fn: func() *DataflowAnalyzer {
				da := NewDataflowAnalyzer()
				da.Define("A", "alpha", false)
				da.Define("B", "beta", false)
				da.Define("C", "gamma", false)
				return da
			},
			in: "$A/$B/$C",
		},
		{
			name: "one_var_five_values",
			fn: func() *DataflowAnalyzer {
				da := NewDataflowAnalyzer()
				for i := 0; i < 5; i++ {
					if i == 0 {
						da.Define("DIR", "v0", false)
						continue
					}
					branch := NewDataflowAnalyzer()
					branch.Define("DIR", "v"+string(rune('0'+i)), false)
					da.MergeBranch(branch)
				}
				return da
			},
			in: "$DIR",
		},
		{
			name: "expansion_limit_hit",
			fn: func() *DataflowAnalyzer {
				da := NewDataflowAnalyzer()
				for i := 0; i < 3; i++ {
					name := "V" + string(rune('A'+i))
					for j := 0; j < 5; j++ {
						if j == 0 {
							da.Define(name, "x0", false)
							continue
						}
						branch := NewDataflowAnalyzer()
						branch.Define(name, "x"+string(rune('0'+j)), false)
						da.MergeBranch(branch)
					}
				}
				return da
			},
			in: "$VA $VB $VC",
		},
	}

	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			da := tc.fn()
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.in)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = da.ResolveString(tc.in)
			}
		})
	}
}

func BenchmarkFullPipeline(b *testing.B) {
	p := NewBashParser()
	ctx := context.Background()
	for _, tc := range benchmarkCommandCases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = p.ParseAndExtract(ctx, tc.input, 0)
			}
		})
	}
}

func BenchmarkParsePoolEffectiveness(b *testing.B) {
	input := strings.Repeat("echo hello && ", 32) + "rm -rf /tmp/x"
	ctx := context.Background()
	pooled := NewBashParser()

	b.Run("with_pool", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = pooled.Parse(ctx, input)
		}
	})

	b.Run("without_pool", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(input)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			p := tsp.NewParser()
			p.SetLanguage(bash.Language())
			raw := p.ParseString(ctx, []byte(input))
			_ = NewTree(raw)
		}
	})
}

func BenchmarkInlineDetectionOverhead(b *testing.B) {
	p := NewBashParser()
	ctx := context.Background()
	inline := NewInlineDetector(p)
	extractor := NewCommandExtractor(p)

	cases := []struct {
		name  string
		input string
	}{
		{name: "no_inline", input: "rm -rf /tmp/data"},
		{name: "python_inline", input: `python -c "import os; os.system('rm -rf /tmp/data')"`},
		{name: "bash_inline", input: `bash -c "rm -rf /tmp/data"`},
	}

	for _, tc := range cases {
		tc := tc
		tree, _ := p.Parse(ctx, tc.input)
		if tree == nil {
			continue
		}
		result := extractor.Extract(tree, tc.input)
		if len(result.Commands) == 0 {
			continue
		}
		cmd := result.Commands[0]

		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.input)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = inline.Detect(cmd, 0)
			}
		})
	}
}
