package packs

import (
	"regexp"
	"strings"
)

// Command is the matcher-facing command model consumed by pack rules.
// Eval converts parser output into this shape.
type Command struct {
	Name    string
	Args    []string
	RawArgs []string
	Flags   map[string]string
	RawText string
}

// CommandMatcher evaluates whether a command matches a rule predicate.
type CommandMatcher interface {
	Match(Command) bool
}

// MatchFunc adapts a function to the CommandMatcher interface.
type MatchFunc func(Command) bool

func (f MatchFunc) Match(cmd Command) bool {
	if f == nil {
		return false
	}
	return f(cmd)
}

func Name(name string) MatchFunc {
	want := strings.ToLower(strings.TrimSpace(name))
	return MatchFunc(func(cmd Command) bool {
		return strings.EqualFold(cmd.Name, want)
	})
}

func AnyName() MatchFunc {
	return MatchFunc(func(cmd Command) bool {
		return strings.TrimSpace(cmd.Name) != ""
	})
}

func ArgAt(idx int, value string) MatchFunc {
	want := strings.ToLower(value)
	return MatchFunc(func(cmd Command) bool {
		if idx < 0 || idx >= len(cmd.Args) {
			return false
		}
		return strings.EqualFold(cmd.Args[idx], want)
	})
}

// ArgSubsequence matches when all values appear in Args in order.
// Values do not need to be contiguous.
func ArgSubsequence(values ...string) MatchFunc {
	if len(values) == 0 {
		return MatchFunc(func(Command) bool { return false })
	}
	wants := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return MatchFunc(func(Command) bool { return false })
		}
		wants = append(wants, strings.ToLower(trimmed))
	}
	return MatchFunc(func(cmd Command) bool {
		if len(cmd.Args) < len(wants) {
			return false
		}
		pos := 0
		for _, arg := range cmd.Args {
			if strings.EqualFold(arg, wants[pos]) {
				pos++
				if pos == len(wants) {
					return true
				}
			}
		}
		return false
	})
}

// Flags returns true only if all listed flags are present.
func Flags(flags ...string) MatchFunc {
	return MatchFunc(func(cmd Command) bool {
		if len(flags) == 0 {
			return false
		}
		for _, flag := range flags {
			if _, ok := cmd.Flags[flag]; !ok {
				return false
			}
		}
		return true
	})
}

func And(matchers ...CommandMatcher) MatchFunc {
	return MatchFunc(func(cmd Command) bool {
		if len(matchers) == 0 {
			return false
		}
		for _, m := range matchers {
			if m == nil || !m.Match(cmd) {
				return false
			}
		}
		return true
	})
}

func Or(matchers ...CommandMatcher) MatchFunc {
	return MatchFunc(func(cmd Command) bool {
		for _, m := range matchers {
			if m != nil && m.Match(cmd) {
				return true
			}
		}
		return false
	})
}

func Not(m CommandMatcher) MatchFunc {
	return MatchFunc(func(cmd Command) bool {
		if m == nil {
			return true
		}
		return !m.Match(cmd)
	})
}

func Arg(value string) MatchFunc {
	want := strings.ToLower(value)
	return MatchFunc(func(cmd Command) bool {
		for _, arg := range cmd.Args {
			if strings.EqualFold(arg, want) {
				return true
			}
		}
		for _, arg := range cmd.RawArgs {
			if strings.EqualFold(arg, want) {
				return true
			}
		}
		return false
	})
}

func ArgContains(term string) MatchFunc {
	want := strings.ToLower(term)
	return MatchFunc(func(cmd Command) bool {
		for _, arg := range cmd.Args {
			if strings.Contains(strings.ToLower(arg), want) {
				return true
			}
		}
		for _, arg := range cmd.RawArgs {
			if strings.Contains(strings.ToLower(arg), want) {
				return true
			}
		}
		return false
	})
}

func ArgPrefix(prefix string) MatchFunc {
	want := strings.ToLower(prefix)
	return MatchFunc(func(cmd Command) bool {
		for _, arg := range cmd.Args {
			if strings.HasPrefix(strings.ToLower(arg), want) {
				return true
			}
		}
		for _, arg := range cmd.RawArgs {
			if strings.HasPrefix(strings.ToLower(arg), want) {
				return true
			}
		}
		return false
	})
}

func ArgContentRegex(pattern string) MatchFunc {
	re := regexp.MustCompile(pattern)
	return MatchFunc(func(cmd Command) bool {
		for _, arg := range cmd.Args {
			if re.MatchString(arg) {
				return true
			}
		}
		for _, arg := range cmd.RawArgs {
			if re.MatchString(arg) {
				return true
			}
		}
		return false
	})
}

func RawTextContains(term string) MatchFunc {
	want := strings.ToLower(term)
	return MatchFunc(func(cmd Command) bool {
		return strings.Contains(strings.ToLower(cmd.RawText), want)
	})
}

func RawTextRegex(re *regexp.Regexp) MatchFunc {
	return MatchFunc(func(cmd Command) bool {
		return re != nil && re.MatchString(cmd.RawText)
	})
}

func hasAll(cmd Command, terms ...string) bool {
	s := strings.ToLower(cmd.RawText)
	for _, term := range terms {
		if !strings.Contains(s, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

func hasAny(cmd Command, terms ...string) bool {
	s := strings.ToLower(cmd.RawText)
	for _, term := range terms {
		if strings.Contains(s, strings.ToLower(term)) {
			return true
		}
	}
	return false
}
