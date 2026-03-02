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

type astCommandMeta struct {
	inPipeline bool
	negated    bool
}

type sourceCommand struct {
	tokens     []string
	start      int
	end        int
	sepBefore  string
	inPipeline bool
	negated    bool
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

	astMetas := collectASTCommands(tree.RootNode(), false, false)
	lexCommands := splitSourceCommands(source)
	metaIdx := 0
	for _, sc := range lexCommands {
		if hasCommandToken(sc.tokens) && metaIdx < len(astMetas) {
			sc.inPipeline = astMetas[metaIdx].inPipeline
			sc.negated = astMetas[metaIdx].negated
			metaIdx++
		}
		commands, warnings := ce.extractCommand(sc, source)
		result.Commands = append(result.Commands, commands...)
		result.Warnings = append(result.Warnings, warnings...)
	}
	result.ExportedVars = ce.dataflow.ExportedVars()
	return result
}

func hasCommandToken(tokens []string) bool {
	tmp := append([]string{}, tokens...)
	for len(tmp) > 0 && tmp[0] == "!" {
		tmp = tmp[1:]
	}
	for len(tmp) > 0 && isInlineAssignment(tmp[0]) {
		tmp = tmp[1:]
	}
	return len(tmp) > 0
}

func collectASTCommands(node ts.Node, inPipeline bool, negated bool) []astCommandMeta {
	if node.IsNull() {
		return nil
	}

	switch node.Type() {
	case "pipeline":
		var out []astCommandMeta
		for i := 0; i < int(node.ChildCount()); i++ {
			out = append(out, collectASTCommands(node.Child(i), true, negated)...)
		}
		return out
	case "negated_command":
		var out []astCommandMeta
		for i := 0; i < int(node.ChildCount()); i++ {
			out = append(out, collectASTCommands(node.Child(i), inPipeline, true)...)
		}
		return out
	case "command":
		return []astCommandMeta{{inPipeline: inPipeline, negated: negated}}
	}

	var out []astCommandMeta
	for i := 0; i < int(node.ChildCount()); i++ {
		out = append(out, collectASTCommands(node.Child(i), inPipeline, negated)...)
	}
	return out
}

func (ce *CommandExtractor) extractCommand(sc sourceCommand, source string) ([]ExtractedCommand, []guard.Warning) {
	base := ExtractedCommand{
		Flags:      map[string]string{},
		InlineEnv:  map[string]string{},
		InPipeline: sc.inPipeline,
		Negated:    sc.negated,
		StartByte:  uint32(sc.start),
		EndByte:    uint32(sc.end),
	}
	if sc.start >= 0 && sc.end >= sc.start && sc.end <= len(source) {
		base.RawText = strings.TrimSpace(source[sc.start:sc.end])
	}

	tokens := append([]string{}, sc.tokens...)
	for len(tokens) > 0 && tokens[0] == "!" {
		base.Negated = true
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	var warnings []guard.Warning
	for len(tokens) > 0 && isInlineAssignment(tokens[0]) {
		k, v, _ := strings.Cut(tokens[0], "=")
		base.InlineEnv[k] = v
		tokens = tokens[1:]
	}

	if len(tokens) == 0 {
		// Bare assignment statement; affects dataflow.
		for k, v := range base.InlineEnv {
			ce.defineVar(k, v, false, sc.sepBefore == "&&" || sc.sepBefore == "||")
		}
		return nil, warnings
	}

	base.RawName = tokens[0]
	base.Name = Normalize(tokens[0])
	if base.Name == "" {
		base.Name = base.RawName
	}

	args := tokens[1:]
	variants := []ExtractedCommand{base}
	for _, arg := range args {
		exps, capped := ce.dataflow.ResolveString(arg)
		if capped {
			warnings = append(warnings, guard.Warning{
				Code:    guard.WarnExpansionCapped,
				Message: "dataflow expansion capped at 16",
			})
		}
		if len(exps) == 0 {
			exps = []string{arg}
		}

		next := make([]ExtractedCommand, 0, len(variants)*len(exps))
		for _, variant := range variants {
			for _, e := range exps {
				nv := cloneCommand(variant)
				nv.RawArgs = append(nv.RawArgs, e)
				classifyArg(e, &nv)
				if len(exps) > 1 || e != arg {
					nv.DataflowResolved = true
				}
				next = append(next, nv)
			}
		}
		variants = next
	}

	mergeMode := sc.sepBefore == "&&" || sc.sepBefore == "||"
	if base.Name == "export" {
		for _, arg := range variants[0].Args {
			k, v, ok := extractAssignment(arg)
			if ok {
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
	}

	// Inline env vars are command-scoped and must not leak into global dataflow.

	for i := range variants {
		unwrapEnvPrefix(&variants[i])
	}
	return variants, warnings
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

func splitSourceCommands(source string) []sourceCommand {
	var (
		commands      []sourceCommand
		tokens        []string
		tokenBuilder  strings.Builder
		inSingleQuote bool
		inDoubleQuote bool
		escaped       bool
		commandStart  = -1
		nextSep       string
		nextPipe      bool
	)

	finalizeToken := func() {
		if tokenBuilder.Len() == 0 {
			return
		}
		tokens = append(tokens, tokenBuilder.String())
		tokenBuilder.Reset()
	}
	finalizeCommand := func(end int, sep string) {
		finalizeToken()
		if len(tokens) > 0 {
			start := commandStart
			if start < 0 {
				start = 0
			}
			commands = append(commands, sourceCommand{
				tokens:     append([]string{}, tokens...),
				start:      start,
				end:        end,
				sepBefore:  nextSep,
				inPipeline: nextPipe,
			})
			if sep == "|" {
				commands[len(commands)-1].inPipeline = true
				nextPipe = true
			} else {
				nextPipe = false
			}
			nextSep = sep
		} else if sep != "|" {
			nextPipe = false
			nextSep = sep
		}
		tokens = nil
		commandStart = -1
	}

	for i := 0; i < len(source); i++ {
		ch := source[i]
		if escaped {
			if commandStart == -1 {
				commandStart = i - 1
			}
			tokenBuilder.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			if commandStart == -1 {
				commandStart = i
			}
			escaped = true
			continue
		}

		if inSingleQuote {
			if ch == '\'' {
				inSingleQuote = false
			} else {
				tokenBuilder.WriteByte(ch)
			}
			continue
		}
		if inDoubleQuote {
			if ch == '"' {
				inDoubleQuote = false
			} else {
				tokenBuilder.WriteByte(ch)
			}
			continue
		}

		switch ch {
		case '\'':
			if commandStart == -1 {
				commandStart = i
			}
			inSingleQuote = true
		case '"':
			if commandStart == -1 {
				commandStart = i
			}
			inDoubleQuote = true
		case ' ', '\t', '\r':
			finalizeToken()
		case '\n', ';':
			finalizeCommand(i, ";")
		case '|':
			if i+1 < len(source) && source[i+1] == '|' {
				finalizeCommand(i, "||")
				i++
			} else {
				finalizeCommand(i, "|")
			}
		case '&':
			if i+1 < len(source) && source[i+1] == '&' {
				finalizeCommand(i, "&&")
				i++
			} else {
				if commandStart == -1 {
					commandStart = i
				}
				tokenBuilder.WriteByte(ch)
			}
		default:
			if commandStart == -1 {
				commandStart = i
			}
			tokenBuilder.WriteByte(ch)
		}
	}
	finalizeCommand(len(source), "")
	return commands
}
