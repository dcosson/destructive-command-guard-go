package parse

import (
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
	ts "github.com/dcosson/treesitter-go"
)

type CommandExtractor struct {
	bashParser *BashParser
	dataflow   *DataflowAnalyzer
}

type commandNodeInfo struct {
	node       ts.Node
	inPipeline bool
	negated    bool
}

type sourceCommand struct {
	tokens    []string
	sepBefore string
}

type lexToken struct {
	kind string // word|op
	text string
}

func NewCommandExtractor(bp *BashParser) *CommandExtractor {
	return &CommandExtractor{
		bashParser: bp,
		dataflow:   NewDataflowAnalyzer(),
	}
}

func (ce *CommandExtractor) Extract(tree *Tree, source string) (result ParseResult) {
	result.ExportedVars = map[string][]string{}
	if tree == nil {
		return result
	}

	defer func() {
		if r := recover(); r != nil {
			result.Warnings = append(result.Warnings, guard.Warning{
				Code:    guard.WarnExtractorPanic,
				Message: "extractor panic recovered",
			})
		}
	}()

	nodes := collectASTCommandNodes(tree.RootNode(), false, false)
	fallback := splitSourceCommands(source)
	nodeIdx := 0
	for _, fb := range fallback {
		if isBareAssignmentCommand(fb.tokens) {
			ws := ce.applyBareAssignment(fb)
			result.Warnings = append(result.Warnings, ws...)
			continue
		}
		if nodeIdx >= len(nodes) {
			continue
		}
		cmds, warnings := ce.extractFromASTCommand(nodes[nodeIdx], source, fb)
		result.Commands = append(result.Commands, cmds...)
		result.Warnings = append(result.Warnings, warnings...)
		nodeIdx++
	}
	result.ExportedVars = ce.dataflow.ExportedVars()
	return result
}

func collectASTCommandNodes(node ts.Node, inPipeline bool, negated bool) []commandNodeInfo {
	if node.IsNull() {
		return nil
	}

	switch node.Type() {
	case "pipeline":
		var out []commandNodeInfo
		for i := 0; i < int(node.ChildCount()); i++ {
			out = append(out, collectASTCommandNodes(node.Child(i), true, negated)...)
		}
		return out
	case "negated_command":
		var out []commandNodeInfo
		for i := 0; i < int(node.ChildCount()); i++ {
			out = append(out, collectASTCommandNodes(node.Child(i), inPipeline, true)...)
		}
		return out
	case "command", "declaration_command":
		return []commandNodeInfo{{node: node, inPipeline: inPipeline, negated: negated}}
	}

	var out []commandNodeInfo
	for i := 0; i < int(node.ChildCount()); i++ {
		out = append(out, collectASTCommandNodes(node.Child(i), inPipeline, negated)...)
	}
	return out
}

func isBareAssignmentCommand(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if !isInlineAssignment(tok) {
			return false
		}
	}
	return true
}

func (ce *CommandExtractor) applyBareAssignment(fb sourceCommand) []guard.Warning {
	var warnings []guard.Warning
	mergeMode := fb.sepBefore == "&&" || fb.sepBefore == "||" || fb.sepBefore == "|"
	for _, tok := range fb.tokens {
		k, v, ok := extractAssignment(tok)
		if !ok {
			continue
		}
		if containsCommandSubstitution(v) {
			ce.defineIndeterminate(k, false, mergeMode)
			warnings = append(warnings, guard.Warning{
				Code:    guard.WarnCommandSubstitution,
				Message: "variable assigned via command substitution",
			})
			continue
		}
		ce.defineVar(k, v, false, mergeMode)
	}
	return warnings
}

func (ce *CommandExtractor) extractFromASTCommand(info commandNodeInfo, source string, fb sourceCommand) ([]ExtractedCommand, []guard.Warning) {
	base := ExtractedCommand{
		Flags:      map[string]string{},
		InlineEnv:  map[string]string{},
		InPipeline: info.inPipeline,
		Negated:    info.negated,
		StartByte:  info.node.StartByte(),
		EndByte:    info.node.EndByte(),
		RawText:    nodeText(source, info.node),
	}
	var warnings []guard.Warning

	required := 0
	for i := 0; i < int(info.node.NamedChildCount()); i++ {
		child := info.node.NamedChild(i)
		if child.Type() == "variable_assignment" || child.Type() == "command_name" || isArgumentNodeType(child.Type()) {
			required++
		}
	}

	tokens := wordTokens(base.RawText)
	if len(tokens) < required {
		tokens = append([]string{}, fb.tokens...)
	}
	if info.node.Type() == "declaration_command" {
		for len(tokens) > 0 {
			switch tokens[0] {
			case "export", "declare", "local", "typeset":
				tokens = tokens[1:]
			default:
				goto doneDeclStrip
			}
		}
	}
doneDeclStrip:
	pos := 0
	nextToken := func() (string, bool) {
		if pos >= len(tokens) {
			return "", false
		}
		t := tokens[pos]
		pos++
		return t, true
	}

	variants := []ExtractedCommand{base}
	sepBefore := fb.sepBefore
	for i := 0; i < int(info.node.NamedChildCount()); i++ {
		child := info.node.NamedChild(i)
		switch child.Type() {
		case "variable_assignment":
			tok, ok := nextToken()
			if !ok {
				continue
			}
			k, v, ok := extractAssignment(tok)
			if !ok {
				continue
			}
			for idx := range variants {
				variants[idx].InlineEnv[k] = v
			}
		case "command_name":
			tok, ok := nextToken()
			if !ok {
				continue
			}
			for idx := range variants {
				variants[idx].RawName = tok
				variants[idx].Name = Normalize(tok)
				if variants[idx].Name == "" {
					variants[idx].Name = tok
				}
			}
		default:
			if !isArgumentNodeType(child.Type()) {
				continue
			}
			tok, ok := nextToken()
			if !ok {
				continue
			}
			exps, capped := ce.dataflow.ResolveString(tok)
			if capped {
				warnings = append(warnings, guard.Warning{
					Code:    guard.WarnExpansionCapped,
					Message: "dataflow expansion capped at 16",
				})
			}
			if len(exps) == 0 {
				exps = []string{tok}
			}
			next := make([]ExtractedCommand, 0, len(variants)*len(exps))
			for _, v := range variants {
				for _, e := range exps {
					nv := cloneCommand(v)
					nv.RawArgs = append(nv.RawArgs, e)
					classifyArg(e, &nv)
					if len(exps) > 1 || e != tok {
						nv.DataflowResolved = true
					}
					next = append(next, nv)
				}
			}
			variants = next
		}
	}

	mergeMode := sepBefore == "&&" || sepBefore == "||" || sepBefore == "|"
	if variants[0].Name == "" {
		for k, v := range variants[0].InlineEnv {
			if containsCommandSubstitution(v) {
				ce.defineIndeterminate(k, false, mergeMode)
				warnings = append(warnings, guard.Warning{
					Code:    guard.WarnCommandSubstitution,
					Message: "variable assigned via command substitution",
				})
			} else {
				ce.defineVar(k, v, false, mergeMode)
			}
		}
		return nil, warnings
	}

	if variants[0].Name == "export" {
		for _, arg := range variants[0].Args {
			k, v, ok := extractAssignment(arg)
			if !ok {
				continue
			}
			if containsCommandSubstitution(v) {
				ce.defineIndeterminate(k, true, mergeMode)
				warnings = append(warnings, guard.Warning{
					Code:    guard.WarnCommandSubstitution,
					Message: "variable assigned via command substitution",
				})
			} else {
				ce.defineVar(k, v, true, mergeMode)
			}
		}
	}

	// Inline env vars are command-scoped only.
	for i := range variants {
		unwrapEnvPrefix(&variants[i])
	}
	return variants, warnings
}

func isArgumentNodeType(nodeType string) bool {
	switch nodeType {
	case "word", "string", "raw_string", "concatenation", "expansion", "simple_expansion", "command_substitution":
		return true
	default:
		return false
	}
}

func wordTokens(raw string) []string {
	toks := lexSourceTokens(raw)
	out := make([]string, 0, len(toks))
	for _, tok := range toks {
		if tok.kind == "word" {
			out = append(out, tok.text)
		}
	}
	return out
}

func splitSourceCommands(source string) []sourceCommand {
	var out []sourceCommand
	toks := lexSourceTokens(source)
	var current []string
	sepBefore := ""

	flush := func(nextSep string) {
		if len(current) > 0 {
			out = append(out, sourceCommand{
				tokens:    append([]string{}, current...),
				sepBefore: sepBefore,
			})
			current = nil
		}
		sepBefore = nextSep
	}

	for _, tok := range toks {
		if tok.kind == "op" {
			flush(tok.text)
			continue
		}
		current = append(current, tok.text)
	}
	flush("")
	return out
}

func lexSourceTokens(source string) []lexToken {
	var (
		out        []lexToken
		buf        strings.Builder
		inSingle   bool
		inDouble   bool
		escaped    bool
		appendWord = func() {
			if buf.Len() == 0 {
				return
			}
			out = append(out, lexToken{kind: "word", text: buf.String()})
			buf.Reset()
		}
		appendOp = func(op string) {
			out = append(out, lexToken{kind: "op", text: op})
		}
	)

	for i := 0; i < len(source); i++ {
		ch := source[i]
		if escaped {
			buf.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}

		if inSingle {
			if ch == '\'' {
				inSingle = false
			} else {
				buf.WriteByte(ch)
			}
			continue
		}
		if inDouble {
			if ch == '"' {
				inDouble = false
			} else {
				buf.WriteByte(ch)
			}
			continue
		}

		switch ch {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case ' ', '\t', '\r':
			appendWord()
		case '\n', ';':
			appendWord()
			appendOp(";")
		case '|':
			appendWord()
			if i+1 < len(source) && source[i+1] == '|' {
				appendOp("||")
				i++
			} else {
				appendOp("|")
			}
		case '&':
			appendWord()
			if i+1 < len(source) && source[i+1] == '&' {
				appendOp("&&")
				i++
			} else {
				buf.WriteByte(ch)
			}
		default:
			buf.WriteByte(ch)
		}
	}
	appendWord()
	return out
}

func cloneCommand(in ExtractedCommand) ExtractedCommand {
	out := in
	out.Args = append([]string{}, in.Args...)
	out.RawArgs = append([]string{}, in.RawArgs...)
	out.Flags = make(map[string]string, len(in.Flags))
	for k, v := range in.Flags {
		out.Flags[k] = v
	}
	out.InlineEnv = make(map[string]string, len(in.InlineEnv))
	for k, v := range in.InlineEnv {
		out.InlineEnv[k] = v
	}
	return out
}

func (ce *CommandExtractor) defineVar(name, value string, exported bool, merge bool) {
	if merge {
		branch := NewDataflowAnalyzer()
		branch.Define(name, value, exported)
		ce.dataflow.MergeBranch(branch)
		return
	}
	ce.dataflow.Define(name, value, exported)
}

func (ce *CommandExtractor) defineIndeterminate(name string, exported bool, merge bool) {
	if merge {
		branch := NewDataflowAnalyzer()
		branch.DefineIndeterminate(name, exported)
		ce.dataflow.MergeBranch(branch)
		return
	}
	ce.dataflow.DefineIndeterminate(name, exported)
}

func classifyArg(text string, cmd *ExtractedCommand) {
	if strings.HasPrefix(text, "--") {
		if idx := strings.IndexByte(text, '='); idx >= 0 {
			cmd.Flags[text[:idx]] = text[idx+1:]
		} else {
			cmd.Flags[text] = ""
		}
		return
	}
	if strings.HasPrefix(text, "-") && len(text) > 1 && text[1] != '-' {
		if !isASCII(text[1:]) {
			cmd.Flags[text] = ""
			return
		}
		for _, c := range text[1:] {
			cmd.Flags["-"+string(c)] = ""
		}
		return
	}
	cmd.Args = append(cmd.Args, text)
}

func extractAssignment(text string) (string, string, bool) {
	key, value, ok := strings.Cut(text, "=")
	if !ok || key == "" {
		return "", "", false
	}
	return key, value, true
}

func containsCommandSubstitution(s string) bool {
	return strings.Contains(s, "$(") || strings.Contains(s, "`")
}
