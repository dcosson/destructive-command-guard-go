package parse

import (
	"strings"
	"sync"
	"unicode"
)

var normalizeIntern sync.Map

func Normalize(name string) string {
	if name == "" {
		return ""
	}
	if v, ok := normalizeIntern.Load(name); ok {
		return v.(string)
	}

	normalized := name
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		normalized = name[idx+1:]
	}
	if normalized == "" {
		return ""
	}

	// Use the normalized value as the canonical key to increase cache hits.
	if v, ok := normalizeIntern.Load(normalized); ok {
		return v.(string)
	}
	normalizeIntern.Store(normalized, normalized)
	return normalized
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
