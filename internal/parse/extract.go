package parse

import (
	ts "github.com/dcosson/treesitter-go"
	"strings"
)

// CommandExtractor walks a tree-sitter bash AST to discover command structure
// (pipelines, lists, commands, declarations, bare assignments) and produces
// ExtractedCommand values.
//
// ARCHITECTURE NOTE — treesitter-go v0.1.0 byte-offset bug workaround:
//
// treesitter-go v0.1.0 has a bug where child node StartByte/EndByte positions
// are progressively shifted from their correct values. Parent node positions
// and child node SIZES (EndByte−StartByte) are correct, but child absolute
// positions accumulate error with depth and sibling index.
//
// To work around this, the AST walk passes each node's CORRECT text (computed
// from its parent) rather than using nodeText(source, child) directly.
// The root node's text is always correct (it spans the entire source).
// reconstructChildTexts() scans a parent's correct text using child node
// sizes to reconstruct each child's text without relying on child offsets.
//
// Within a single command's text, tokenizeCommand() splits tokens respecting
// quotes. This is safe because the command text comes from the parent-corrected
// pipeline, not from the (potentially broken) command node offsets.
//
// When treesitter-go fixes the byte offset bug, reconstructChildTexts can be
// removed and nodeText(source, child) can be used directly.
type CommandExtractor struct {
	bashParser *BashParser
	dataflow   *DataflowAnalyzer
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
			result.Warnings = append(result.Warnings, Warning{
				Code:    WarnExtractorPanic,
				Message: "extractor panic recovered",
			})
		}
	}()

	// Root node's text is always the full source (correct offsets).
	ce.walkNodeWithText(tree.RootNode(), source, &result, false, false, false)
	result.ExportedVars = ce.dataflow.ExportedVars()
	return result
}

// ---------------------------------------------------------------------------
// AST WALK — determines command boundaries, pipelines, lists, nesting.
//
// Every walk function receives the node's CORRECT text (computed from its
// parent via reconstructChildTexts). This avoids relying on potentially
// broken child byte offsets in treesitter-go v0.1.0.
// ---------------------------------------------------------------------------

func (ce *CommandExtractor) walkNodeWithText(node ts.Node, text string, result *ParseResult, inPipeline, negated, mergeMode bool) {
	if node.IsNull() || text == "" {
		return
	}

	switch node.Type() {
	case "command":
		cmds, warnings := ce.extractCommandFromChildren(node, text, inPipeline, negated, mergeMode, false)
		result.Commands = append(result.Commands, cmds...)
		result.Warnings = append(result.Warnings, warnings...)
		ce.walkNestedCommandContexts(node, text, result, inPipeline, negated, mergeMode)
	case "declaration_command":
		cmds, warnings := ce.extractCommandFromChildren(node, text, inPipeline, negated, mergeMode, true)
		result.Commands = append(result.Commands, cmds...)
		result.Warnings = append(result.Warnings, warnings...)
		ce.walkNestedCommandContexts(node, text, result, inPipeline, negated, mergeMode)
	case "variable_assignment":
		ce.handleBareAssignment(text, mergeMode, result)
	case "pipeline":
		ce.walkChildrenWithText(node, text, result, true, negated, false)
	case "negated_command":
		ce.walkChildrenWithText(node, text, result, inPipeline, true, mergeMode)
	case "list":
		ce.walkListWithText(node, text, result, inPipeline, negated)
	default:
		ce.walkChildrenWithText(node, text, result, inPipeline, negated, false)
	}
}

func (ce *CommandExtractor) walkNestedCommandContexts(node ts.Node, text string, result *ParseResult, inPipeline, negated, mergeMode bool) {
	if node.IsNull() || text == "" {
		return
	}
	texts := reconstructChildTexts(text, node)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			continue
		}
		childText := ""
		if i < len(texts) {
			childText = texts[i]
		}
		switch child.Type() {
		case "command_substitution", "process_substitution":
			ce.walkNodeWithText(child, childText, result, inPipeline, negated, mergeMode)
		case "word", "string", "raw_string", "concatenation", "expansion", "simple_expansion":
			ce.walkNestedCommandContexts(child, childText, result, inPipeline, negated, mergeMode)
		}
	}
}

// walkChildrenWithText reconstructs correct child texts from the parent's
// (correct) text, then walks each named child with its corrected text.
func (ce *CommandExtractor) walkChildrenWithText(parent ts.Node, parentText string, result *ParseResult, inPipeline, negated, mergeMode bool) {
	texts := reconstructChildTexts(parentText, parent)
	for i := 0; i < int(parent.ChildCount()); i++ {
		child := parent.Child(i)
		if !child.IsNamed() {
			continue
		}
		ce.walkNodeWithText(child, texts[i], result, inPipeline, negated, mergeMode)
	}
}

// walkListWithText handles a list node (&&, ||, ; chains). Uses ALL children
// (including anonymous connector nodes) to determine merge mode.
func (ce *CommandExtractor) walkListWithText(node ts.Node, text string, result *ParseResult, inPipeline, negated bool) {
	texts := reconstructChildTexts(text, node)
	prevConnector := ""
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			prevConnector = strings.TrimSpace(texts[i])
			continue
		}
		merge := prevConnector == "&&" || prevConnector == "||"
		ce.walkNodeWithText(child, texts[i], result, inPipeline, negated, merge)
	}
}

// reconstructChildTexts takes a parent's correct text and reconstructs the
// text for each child by scanning forward using child node sizes (which are
// correct in treesitter-go v0.1.0 even when absolute offsets are wrong).
// Inter-child gaps (whitespace not modeled as tree-sitter nodes) are skipped.
func reconstructChildTexts(parentText string, parent ts.Node) []string {
	n := int(parent.ChildCount())
	texts := make([]string, n)
	pos := 0

	for i := 0; i < n; i++ {
		child := parent.Child(i)
		size := int(child.EndByte()) - int(child.StartByte())
		if size <= 0 {
			continue
		}

		// Skip whitespace between children (tree-sitter does not model
		// inter-token whitespace as nodes).
		for pos < len(parentText) && isInterTokenWhitespace(parentText[pos]) {
			pos++
		}

		end := pos + size
		if end <= len(parentText) {
			texts[i] = parentText[pos:end]
			pos = end
		}
	}
	return texts
}

func isInterTokenWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// extractCommandFromChildren extracts command data from AST child node texts.
func (ce *CommandExtractor) extractCommandFromChildren(node ts.Node, cmdText string, inPipeline, negated, mergeMode, isDeclaration bool) ([]ExtractedCommand, []Warning) {
	base := ExtractedCommand{
		Flags:      map[string]string{},
		InlineEnv:  map[string]string{},
		InPipeline: inPipeline,
		Negated:    negated,
		RawText:    cmdText,
		StartByte:  node.StartByte(),
		EndByte:    node.EndByte(),
	}
	var warnings []Warning

	childTexts := reconstructChildTexts(cmdText, node)
	var fallback []string
	if len(childTexts) == 0 {
		fallback = tokenizeCommand(cmdText) // last-resort recovery only
	}
	fallbackPos := 0
	nextFallback := func() (string, bool) {
		if fallbackPos >= len(fallback) {
			return "", false
		}
		t := fallback[fallbackPos]
		fallbackPos++
		return t, true
	}
	childText := func(i int) (string, bool) {
		if i < len(childTexts) {
			if t := normalizeCommandText(childTexts[i]); t != "" {
				return t, true
			}
		}
		return nextFallback()
	}

	var declarationName string
	if isDeclaration {
		declarationName = firstToken(cmdText)
		if declarationName != "" {
			base.RawName = declarationName
			base.Name = declarationName
		}
	}

	variants := []ExtractedCommand{base}
	seenCommandName := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			continue
		}
		text, ok := childText(i)
		if !ok {
			continue
		}
		switch child.Type() {
		case "variable_assignment":
			k, v, ok := extractAssignment(text)
			if !ok {
				continue
			}
			if !isDeclaration && !seenCommandName {
				for idx := range variants {
					variants[idx].InlineEnv[k] = v
				}
				continue
			}
			exported := isDeclaration && declarationName == "export"
			if containsCommandSubstitution(v) {
				ce.defineIndeterminate(k, exported, mergeMode)
				warnings = append(warnings, Warning{
					Code:    WarnCommandSubstitution,
					Message: "variable assigned via command substitution",
				})
			} else {
				ce.defineVar(k, v, exported, mergeMode)
			}
			for idx := range variants {
				variants[idx].RawArgs = append(variants[idx].RawArgs, text)
				classifyArg(text, &variants[idx])
			}
		case "command_name":
			seenCommandName = true
			for idx := range variants {
				variants[idx].RawName = text
				variants[idx].Name = Normalize(text)
				if variants[idx].Name == "" {
					variants[idx].Name = text
				}
			}
		default:
			if !isArgumentNodeType(child.Type()) {
				continue
			}
			exps, capped := ce.dataflow.ResolveString(text)
			if capped {
				warnings = append(warnings, Warning{
					Code:    WarnExpansionCapped,
					Message: "dataflow expansion capped at 16",
				})
			}
			if len(exps) == 0 {
				exps = []string{text}
			}
			next := make([]ExtractedCommand, 0, len(variants)*len(exps))
			for _, v := range variants {
				for _, e := range exps {
					nv := cloneCommand(v)
					nv.RawArgs = append(nv.RawArgs, e)
					classifyArg(e, &nv)
					if len(exps) > 1 || e != text {
						nv.DataflowResolved = true
					}
					next = append(next, nv)
				}
			}
			variants = next
		}
	}

	if isDeclaration {
		return variants, warnings
	}

	if variants[0].Name == "" {
		// Only inline assignments, no command — track in dataflow.
		for k, v := range variants[0].InlineEnv {
			if containsCommandSubstitution(v) {
				ce.defineIndeterminate(k, false, mergeMode)
				warnings = append(warnings, Warning{
					Code:    WarnCommandSubstitution,
					Message: "variable assigned via command substitution",
				})
			} else {
				ce.defineVar(k, v, false, mergeMode)
			}
		}
		return nil, warnings
	}

	// Handle export commands — track assignments in dataflow.
	if variants[0].Name == "export" {
		for _, arg := range variants[0].Args {
			k, v, ok := extractAssignment(arg)
			if !ok {
				continue
			}
			if containsCommandSubstitution(v) {
				ce.defineIndeterminate(k, true, mergeMode)
				warnings = append(warnings, Warning{
					Code:    WarnCommandSubstitution,
					Message: "variable assigned via command substitution",
				})
			} else {
				ce.defineVar(k, v, true, mergeMode)
			}
		}
	}

	// Inline env vars are command-scoped; unwrap env prefix if applicable.
	for i := range variants {
		unwrapEnvPrefix(&variants[i])
	}
	return variants, warnings
}

// handleBareAssignment processes a standalone variable_assignment node's text.
func (ce *CommandExtractor) handleBareAssignment(text string, mergeMode bool, result *ParseResult) {
	k, v, ok := extractAssignment(text)
	if !ok {
		return
	}
	if containsCommandSubstitution(v) {
		ce.defineIndeterminate(k, false, mergeMode)
		result.Warnings = append(result.Warnings, Warning{
			Code:    WarnCommandSubstitution,
			Message: "variable assigned via command substitution",
		})
	} else {
		ce.defineVar(k, v, false, mergeMode)
	}
}

// ---------------------------------------------------------------------------
// TOKENIZER — splits a single command's text into tokens, respecting quotes.
// Used only within a single command node's text, NOT the entire source.
// ---------------------------------------------------------------------------

func tokenizeCommand(text string) []string {
	var (
		tokens   []string
		buf      strings.Builder
		inSingle bool
		inDouble bool
		escaped  bool
	)

	flush := func() {
		if buf.Len() > 0 {
			tokens = append(tokens, buf.String())
			buf.Reset()
		}
	}

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if escaped {
			buf.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
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
		case ' ', '\t':
			flush()
		default:
			buf.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

func firstToken(text string) string {
	toks := tokenizeCommand(text)
	if len(toks) == 0 {
		return ""
	}
	return toks[0]
}

// ---------------------------------------------------------------------------
// HELPERS
// ---------------------------------------------------------------------------

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

func isArgumentNodeType(nodeType string) bool {
	switch nodeType {
	case "word", "string", "raw_string", "concatenation", "expansion", "simple_expansion", "command_substitution":
		return true
	default:
		return false
	}
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
