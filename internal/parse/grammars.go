package parse

import (
	"context"
	"sync"

	ts "github.com/dcosson/treesitter-go"
	"github.com/dcosson/treesitter-go/languages/bash"
	"github.com/dcosson/treesitter-go/languages/javascript"
	"github.com/dcosson/treesitter-go/languages/lua"
	"github.com/dcosson/treesitter-go/languages/perl"
	"github.com/dcosson/treesitter-go/languages/python"
	"github.com/dcosson/treesitter-go/languages/ruby"
	"github.com/dcosson/treesitter-go/parser"
)

type LangGrammar struct {
	Name   string
	Loader func() *ts.Language
}

var SupportedLanguages = []LangGrammar{
	{Name: "bash", Loader: bash.Language},
	{Name: "python", Loader: python.Language},
	{Name: "ruby", Loader: ruby.Language},
	{Name: "javascript", Loader: javascript.Language},
	{Name: "perl", Loader: perl.Language},
	{Name: "lua", Loader: lua.Language},
}

type LangParser struct {
	Grammar LangGrammar
	pool    sync.Pool
}

func NewLangParser(grammar LangGrammar) *LangParser {
	lp := &LangParser{Grammar: grammar}
	lp.pool.New = func() any {
		p := parser.NewParser()
		p.SetLanguage(grammar.Loader())
		return p
	}
	return lp
}

func (lp *LangParser) Parse(ctx context.Context, source []byte) *ts.Tree {
	p := lp.pool.Get().(*parser.Parser)
	defer lp.pool.Put(p)
	return p.ParseString(ctx, source)
}
