package parse

import "strings"

// DataflowAnalyzer in dcg-otc.2 is intentionally minimal and sequential-only.
// dcg-otc.3 expands this to branch-aware over-approximation.
type DataflowAnalyzer struct {
	defs    map[string]string
	exports map[string]bool
}

func NewDataflowAnalyzer() *DataflowAnalyzer {
	return &DataflowAnalyzer{
		defs:    make(map[string]string),
		exports: make(map[string]bool),
	}
}

func (da *DataflowAnalyzer) Define(name, value string, exported bool) {
	if name == "" {
		return
	}
	da.defs[name] = value
	if exported {
		da.exports[name] = true
	}
}

func (da *DataflowAnalyzer) Resolve(name string) (string, bool) {
	v, ok := da.defs[name]
	return v, ok
}

func (da *DataflowAnalyzer) ResolveString(text string) (string, bool) {
	if !strings.Contains(text, "$") {
		return text, false
	}

	resolved := text
	changed := false
	for name, value := range da.defs {
		for _, marker := range []string{"$" + name, "${" + name + "}"} {
			if strings.Contains(resolved, marker) {
				resolved = strings.ReplaceAll(resolved, marker, value)
				changed = true
			}
		}
	}
	return resolved, changed
}

func (da *DataflowAnalyzer) ExportedVars() map[string][]string {
	out := make(map[string][]string, len(da.exports))
	for name := range da.exports {
		out[name] = []string{da.defs[name]}
	}
	return out
}
