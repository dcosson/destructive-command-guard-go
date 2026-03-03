package parse

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/dcosson/destructive-command-guard-go/guard"
	ts "github.com/dcosson/treesitter-go"
)

const MaxInlineDepth = 3

type inlineRule struct {
	Command  string
	Flags    []string
	Language string
}

var inlineRules = []inlineRule{
	{Command: "bash", Flags: []string{"-c"}, Language: "bash"},
	{Command: "sh", Flags: []string{"-c"}, Language: "bash"},
	{Command: "zsh", Flags: []string{"-c"}, Language: "bash"},
	{Command: "eval", Flags: nil, Language: "bash"},
	{Command: "python", Flags: []string{"-c"}, Language: "python"},
	{Command: "python3", Flags: []string{"-c"}, Language: "python"},
	{Command: "ruby", Flags: []string{"-e"}, Language: "ruby"},
	{Command: "perl", Flags: []string{"-e", "-E"}, Language: "perl"},
	{Command: "node", Flags: []string{"-e", "--eval"}, Language: "javascript"},
	{Command: "lua", Flags: []string{"-e"}, Language: "lua"},
}

var (
	pythonShellREs = []*regexp.Regexp{
		regexp.MustCompile(`os\.system\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`subprocess\.(?:run|call|Popen)\(\s*['"]([^'"]+)['"]`),
	}
	rubyShellREs = []*regexp.Regexp{
		regexp.MustCompile("`([^`]+)`"),
		regexp.MustCompile(`(?:system|exec)\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`%x\{\s*([^}]+)\s*\}`),
	}
	jsShellREs = []*regexp.Regexp{
		regexp.MustCompile(`(?:exec|execSync)\(\s*['"]([^'"]+)['"]\s*\)`),
	}
	perlShellREs = []*regexp.Regexp{
		regexp.MustCompile("`([^`]+)`"),
		regexp.MustCompile(`(?:system|exec)\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`qx\{\s*([^}]+)\s*\}`),
	}
	luaShellREs = []*regexp.Regexp{
		regexp.MustCompile(`os\.execute\(\s*['"]([^'"]+)['"]\s*\)`),
	}
	heredocStartRE = regexp.MustCompile(`^\s*(bash|sh|zsh)\b.*<<[-~]?['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?\s*$`)
	catPipeStartRE = regexp.MustCompile(`^\s*cat\b.*<<[-~]?['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?\s*\|\s*(bash|sh|zsh)\b.*$`)
)

type InlineDetector struct {
	parsers    map[string]*LangParser
	mu         sync.Mutex
	bashParser *BashParser
}

func NewInlineDetector(bp *BashParser) *InlineDetector {
	return &InlineDetector{
		parsers:    make(map[string]*LangParser),
		bashParser: bp,
	}
}

func (id *InlineDetector) getParser(lang string) *LangParser {
	id.mu.Lock()
	defer id.mu.Unlock()

	if p, ok := id.parsers[lang]; ok {
		return p
	}
	for _, g := range SupportedLanguages {
		if g.Name == lang {
			p := NewLangParser(g)
			id.parsers[lang] = p
			return p
		}
	}
	return nil
}

func (id *InlineDetector) Detect(cmd ExtractedCommand, depth int) ([]ExtractedCommand, []guard.Warning) {
	if depth >= MaxInlineDepth {
		return nil, []guard.Warning{{
			Code:    guard.WarnInlineDepthExceeded,
			Message: fmt.Sprintf("inline recursion depth %d exceeds max %d", depth, MaxInlineDepth),
		}}
	}

	scripts := id.detectFlagScripts(cmd)
	var out []ExtractedCommand
	var warns []guard.Warning
	for _, script := range scripts {
		switch script.Language {
		case "bash":
			res := id.bashParser.ParseAndExtract(context.Background(), script.Content, depth+1)
			out = append(out, res.Commands...)
			warns = append(warns, res.Warnings...)
		default:
			lp := id.getParser(script.Language)
			shellCmds := id.extractShellInvocationsFromAST(lp, script.Language, script.Content)
			if len(shellCmds) == 0 {
				// Fallback for parse errors or unsupported AST forms.
				shellCmds = id.extractShellInvocationsRegex(script.Language, script.Content)
			}
			for _, shellCmd := range shellCmds {
				res := id.bashParser.ParseAndExtract(context.Background(), shellCmd, depth+1)
				out = append(out, res.Commands...)
				warns = append(warns, res.Warnings...)
			}
		}
	}
	return out, warns
}

func (id *InlineDetector) DetectHeredocs(input string, depth int) ([]ExtractedCommand, []guard.Warning) {
	if depth >= MaxInlineDepth {
		return nil, []guard.Warning{{
			Code:    guard.WarnInlineDepthExceeded,
			Message: fmt.Sprintf("inline recursion depth %d exceeds max %d", depth, MaxInlineDepth),
		}}
	}

	var out []ExtractedCommand
	var warns []guard.Warning
	for _, body := range extractHeredocBodies(input) {
		res := id.bashParser.ParseAndExtract(context.Background(), body, depth+1)
		out = append(out, res.Commands...)
		warns = append(warns, res.Warnings...)
	}
	return out, warns
}

func extractHeredocBodies(input string) []string {
	lines := strings.Split(input, "\n")
	var bodies []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		var delim string
		if m := heredocStartRE.FindStringSubmatch(line); len(m) > 2 {
			delim = m[2]
		} else if m := catPipeStartRE.FindStringSubmatch(line); len(m) > 1 {
			delim = m[1]
		}
		if delim == "" {
			continue
		}

		start := i + 1
		end := -1
		for j := start; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == delim {
				end = j
				break
			}
		}
		if end == -1 || end <= start {
			continue
		}
		bodies = append(bodies, strings.Join(lines[start:end], "\n"))
		i = end
	}
	return bodies
}

func (id *InlineDetector) detectFlagScripts(cmd ExtractedCommand) []InlineScript {
	var scripts []InlineScript
	for _, rule := range inlineRules {
		if cmd.Name != rule.Command {
			continue
		}
		if rule.Flags == nil {
			if len(cmd.Args) == 0 {
				continue
			}
			scripts = append(scripts, InlineScript{
				Language: rule.Language,
				Content:  strings.Join(cmd.Args, " "),
				Source:   "eval",
			})
			continue
		}
		for i := 0; i < len(cmd.RawArgs); i++ {
			arg := cmd.RawArgs[i]
			for _, flag := range rule.Flags {
				if arg == flag && i+1 < len(cmd.RawArgs) && cmd.RawArgs[i+1] != "" {
					scripts = append(scripts, InlineScript{
						Language: rule.Language,
						Content:  cmd.RawArgs[i+1],
						Source:   "flag",
					})
					break
				}
				if strings.HasPrefix(arg, flag+"=") {
					scripts = append(scripts, InlineScript{
						Language: rule.Language,
						Content:  strings.TrimPrefix(arg, flag+"="),
						Source:   "flag",
					})
					break
				}
			}
		}
	}
	return scripts
}

func (id *InlineDetector) extractShellInvocationsFromAST(lp *LangParser, language, script string) []string {
	if lp == nil {
		return nil
	}
	tree := lp.Parse(context.Background(), []byte(script))
	if tree == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var out []string
	var walk func(node ts.Node, nodeText string)
	walk = func(node ts.Node, nodeText string) {
		if node.IsNull() {
			return
		}
		if couldContainShellInvocation(language, node.Type()) {
			for _, cmd := range id.extractShellInvocationsRegex(language, nodeText) {
				if _, ok := seen[cmd]; ok {
					continue
				}
				seen[cmd] = struct{}{}
				out = append(out, cmd)
			}
		}

		// Reconstruct child texts from parent text using node child sizes.
		childTexts := reconstructChildTexts(nodeText, node)
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			ct := ""
			if i < len(childTexts) {
				ct = childTexts[i]
			}
			walk(child, ct)
		}
	}
	// Root text is the original script; child text reconstruction handles offset drift.
	walk(tree.RootNode(), script)
	return out
}

func couldContainShellInvocation(language, nodeType string) bool {
	switch language {
	case "python", "javascript":
		return nodeType == "call" || nodeType == "expression_statement"
	case "ruby", "perl":
		return nodeType == "call" || nodeType == "command" || nodeType == "method_call" || nodeType == "program"
	case "lua":
		return nodeType == "function_call" || nodeType == "call" || nodeType == "statement"
	default:
		return true
	}
}

func (id *InlineDetector) extractShellInvocationsRegex(language, script string) []string {
	var res []string
	var regexes []*regexp.Regexp
	switch language {
	case "python":
		regexes = pythonShellREs
	case "ruby":
		regexes = rubyShellREs
	case "javascript":
		regexes = jsShellREs
	case "perl":
		regexes = perlShellREs
	case "lua":
		regexes = luaShellREs
	default:
		return nil
	}
	for _, re := range regexes {
		matches := re.FindAllStringSubmatch(script, -1)
		for _, m := range matches {
			if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
				res = append(res, strings.TrimSpace(m[1]))
			}
		}
	}
	return res
}
