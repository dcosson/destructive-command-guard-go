package parse

import (
	"context"
	"fmt"
	"sync"

	"github.com/dcosson/destructive-command-guard-go/guard"
	ts "github.com/dcosson/treesitter-go"
	"github.com/dcosson/treesitter-go/languages/bash"
	tsp "github.com/dcosson/treesitter-go/parser"
)

const MaxInputSize = 128 * 1024

type BashParser struct {
	pool sync.Pool
}

func NewBashParser() *BashParser {
	bp := &BashParser{}
	bp.pool.New = func() any {
		// treesitter-go v0.1.0 does not expose ts.NewParser at root; use parser facade.
		p := tsp.NewParser()
		p.SetLanguage(bash.Language())
		return p
	}
	return bp
}

func (bp *BashParser) Parse(ctx context.Context, input string) (tree *Tree, warnings []guard.Warning) {
	if len(input) > MaxInputSize {
		return nil, []guard.Warning{{
			Code:    guard.WarnInputTruncated,
			Message: fmt.Sprintf("input size %d exceeds max size %d", len(input), MaxInputSize),
		}}
	}

	p := bp.pool.Get().(*tsp.Parser)
	shouldReturn := true
	defer func() {
		if r := recover(); r != nil {
			// Do not return panicked parser to the pool; state may be corrupt.
			shouldReturn = false
			tree = nil
			warnings = append(warnings, guard.Warning{
				Code:    guard.WarnExtractorPanic,
				Message: fmt.Sprintf("parser panic recovered: %v", r),
			})
		}
		if shouldReturn {
			bp.pool.Put(p)
		}
	}()

	rawTree := p.ParseString(ctx, []byte(input))
	tree = newTree(rawTree)
	if tree == nil {
		return nil, warnings
	}
	if tree.HasParseError() {
		warnings = append(warnings, guard.Warning{
			Code:    guard.WarnPartialParse,
			Message: "input contains parse recovery errors",
		})
	}

	return tree, warnings
}

func (bp *BashParser) ParseAndExtract(ctx context.Context, input string, depth int) ParseResult {
	tree, warnings := bp.Parse(ctx, input)
	if tree == nil {
		return ParseResult{
			Commands:     nil,
			Warnings:     warnings,
			HasError:     hasWarning(warnings, guard.WarnPartialParse),
			ExportedVars: map[string][]string{},
		}
	}

	extractor := NewCommandExtractor(bp)
	result := extractor.Extract(tree, input)
	inline := NewInlineDetector(bp)
	var nested []ExtractedCommand
	var inlineWarnings []guard.Warning
	for _, cmd := range result.Commands {
		cmds, warns := inline.Detect(cmd, depth)
		nested = append(nested, cmds...)
		inlineWarnings = append(inlineWarnings, warns...)
	}
	heredocCmds, heredocWarns := inline.DetectHeredocs(input, depth)
	nested = append(nested, heredocCmds...)
	inlineWarnings = append(inlineWarnings, heredocWarns...)
	result.Commands = append(result.Commands, nested...)
	result.Warnings = append(result.Warnings, inlineWarnings...)
	result.Warnings = append(warnings, result.Warnings...)
	result.HasError = hasWarning(result.Warnings, guard.WarnPartialParse)
	return result
}

func hasWarning(warnings []guard.Warning, code guard.WarningCode) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func nodeText(source string, node ts.Node) string {
	start := int(node.StartByte())
	end := int(node.EndByte())
	if start < 0 || end < start || end > len(source) {
		return ""
	}
	return source[start:end]
}
