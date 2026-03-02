package parse

import (
	"strings"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

type CommandExtractor struct {
	bashParser *BashParser
	dataflow   *DataflowAnalyzer
}

type sourceCommand struct {
	tokens     []string
	start      int
	end        int
	inPipeline bool
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

	commands := splitSourceCommands(source)
	for _, sc := range commands {
		cmd := ce.extractCommand(sc, source)
		if cmd != nil {
			result.Commands = append(result.Commands, *cmd)
		}
	}
	result.ExportedVars = ce.dataflow.ExportedVars()
	return result
}

func (ce *CommandExtractor) extractCommand(sc sourceCommand, source string) *ExtractedCommand {
	cmd := ExtractedCommand{
		Flags:      map[string]string{},
		InlineEnv:  map[string]string{},
		InPipeline: sc.inPipeline,
		StartByte:  uint32(sc.start),
		EndByte:    uint32(sc.end),
	}
	if sc.start >= 0 && sc.end >= sc.start && sc.end <= len(source) {
		cmd.RawText = strings.TrimSpace(source[sc.start:sc.end])
	}

	tokens := append([]string{}, sc.tokens...)
	for len(tokens) > 0 && tokens[0] == "!" {
		cmd.Negated = true
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return nil
	}

	for len(tokens) > 0 && isInlineAssignment(tokens[0]) {
		k, v, _ := strings.Cut(tokens[0], "=")
		cmd.InlineEnv[k] = v
		tokens = tokens[1:]
	}

	if len(tokens) == 0 {
		// Bare assignment command fragment.
		for k, v := range cmd.InlineEnv {
			ce.dataflow.Define(k, v, false)
		}
		return nil
	}

	cmd.RawName = tokens[0]
	cmd.Name = Normalize(tokens[0])
	if cmd.Name == "" {
		cmd.Name = cmd.RawName
	}

	tokens = tokens[1:]
	for _, rawArg := range tokens {
		resolved, changed := ce.dataflow.ResolveString(rawArg)
		if changed {
			cmd.DataflowResolved = true
		}
		cmd.RawArgs = append(cmd.RawArgs, resolved)
		classifyArg(resolved, &cmd)
	}

	if cmd.Name == "export" {
		for _, arg := range cmd.Args {
			k, v, ok := extractAssignment(arg)
			if ok {
				ce.dataflow.Define(k, v, true)
			}
		}
	} else {
		for k, v := range cmd.InlineEnv {
			ce.dataflow.Define(k, v, false)
		}
	}

	unwrapEnvPrefix(&cmd)
	return &cmd
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

func splitSourceCommands(source string) []sourceCommand {
	var (
		commands       []sourceCommand
		tokens         []string
		tokenBuilder   strings.Builder
		inSingleQuote  bool
		inDoubleQuote  bool
		escaped        bool
		commandStart   = -1
		nextInPipeline bool
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
				inPipeline: nextInPipeline,
			})
			if sep == "|" {
				commands[len(commands)-1].inPipeline = true
				nextInPipeline = true
			} else {
				nextInPipeline = false
			}
		} else if sep != "|" {
			nextInPipeline = false
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
			finalizeCommand(i, string(ch))
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
