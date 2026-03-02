package parse

import (
	"context"
	"fmt"
	"sync"

	"github.com/dcosson/treesitter-go/languages/bash"
	"github.com/dcosson/treesitter-go/parser"
)

const MaxInputSize = 128 * 1024

type BashParser struct {
	pool sync.Pool
}

func NewBashParser() *BashParser {
	bp := &BashParser{}
	bp.pool.New = func() any {
		p := parser.NewParser()
		p.SetLanguage(bash.Language())
		return p
	}
	return bp
}

func (bp *BashParser) Parse(ctx context.Context, input string) (_ *Tree, warnings []Warning) {
	if len(input) > MaxInputSize {
		return nil, []Warning{{
			Code:    WarnInputTooLarge,
			Message: fmt.Sprintf("input size %d exceeds max size %d", len(input), MaxInputSize),
		}}
	}

	defer func() {
		if r := recover(); r != nil {
			warnings = append(warnings, Warning{
				Code:    WarnParserPanic,
				Message: fmt.Sprintf("parser panic: %v", r),
			})
		}
	}()

	p := bp.pool.Get().(*parser.Parser)
	defer bp.pool.Put(p)

	tree := p.ParseString(ctx, []byte(input))
	wrapped := newTree(tree)
	if wrapped == nil {
		return nil, warnings
	}
	if wrapped.HasParseError() {
		warnings = append(warnings, Warning{
			Code:    WarnParseError,
			Message: "input contains parse errors",
		})
	}

	return wrapped, warnings
}
