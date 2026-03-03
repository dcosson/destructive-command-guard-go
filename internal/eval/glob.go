package eval

import (
	"regexp"
	"strings"
)

// globMatch matches a command against a user glob pattern.
// '*' matches any chars except command separators (;, |, &).
func globMatch(pattern, command string) bool {
	p := strings.TrimSpace(pattern)
	c := strings.TrimSpace(command)
	if p == "" {
		return false
	}

	var b strings.Builder
	b.WriteString("^")
	for _, r := range p {
		switch r {
		case '*':
			b.WriteString(`[^;|&]*`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(c)
}
