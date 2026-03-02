package parse

import (
	"strings"
	"unicode"
)

func Normalize(name string) string {
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func normalizeCommandText(text string) string {
	unquoted := strings.TrimSpace(text)
	if len(unquoted) >= 2 {
		if (unquoted[0] == '"' && unquoted[len(unquoted)-1] == '"') ||
			(unquoted[0] == '\'' && unquoted[len(unquoted)-1] == '\'') {
			return unquoted[1 : len(unquoted)-1]
		}
	}
	return unquoted
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func isInlineAssignment(s string) bool {
	if !strings.Contains(s, "=") {
		return false
	}
	key, _, found := strings.Cut(s, "=")
	if !found || key == "" {
		return false
	}
	for i, r := range key {
		if i == 0 {
			if !(unicode.IsLetter(r) || r == '_') {
				return false
			}
			continue
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return true
}

func unwrapEnvPrefix(cmd *ExtractedCommand) {
	if cmd.Name != "env" {
		return
	}

	i := 0
	for i < len(cmd.RawArgs) && isInlineAssignment(cmd.RawArgs[i]) {
		key, value, _ := strings.Cut(cmd.RawArgs[i], "=")
		if cmd.InlineEnv == nil {
			cmd.InlineEnv = make(map[string]string)
		}
		cmd.InlineEnv[key] = value
		i++
	}

	if i >= len(cmd.RawArgs) {
		return
	}

	nextRaw := cmd.RawArgs[i]
	nextName := Normalize(nextRaw)
	if nextName == "" {
		nextName = nextRaw
	}

	cmd.RawName = nextRaw
	cmd.Name = nextName
	cmd.RawArgs = append([]string{}, cmd.RawArgs[i+1:]...)
	cmd.Args = nil
	cmd.Flags = map[string]string{}
	for _, arg := range cmd.RawArgs {
		classifyArg(arg, cmd)
	}
}
