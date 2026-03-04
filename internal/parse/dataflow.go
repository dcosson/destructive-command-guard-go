package parse

import "regexp"

const (
	MaxExpansions = 16
	maxExpansions = MaxExpansions
)

var varRefRE = regexp.MustCompile(`\$(?:([A-Za-z_][A-Za-z0-9_]*)|\{([A-Za-z_][A-Za-z0-9_]*)\})`)

type DataflowAnalyzer struct {
	defs    map[string][]string
	exports map[string]bool
}

func NewDataflowAnalyzer() *DataflowAnalyzer {
	return &DataflowAnalyzer{
		defs:    make(map[string][]string),
		exports: make(map[string]bool),
	}
}

func (da *DataflowAnalyzer) Clone() *DataflowAnalyzer {
	clone := NewDataflowAnalyzer()
	for name, vals := range da.defs {
		if vals == nil {
			clone.defs[name] = nil
			continue
		}
		clone.defs[name] = append([]string{}, vals...)
	}
	for name, exported := range da.exports {
		clone.exports[name] = exported
	}
	return clone
}

func (da *DataflowAnalyzer) Define(name, value string, exported bool) {
	if name == "" {
		return
	}
	da.defs[name] = []string{value}
	if exported {
		da.exports[name] = true
	}
}

func (da *DataflowAnalyzer) DefineIndeterminate(name string, exported bool) {
	if name == "" {
		return
	}
	da.defs[name] = nil
	if exported {
		da.exports[name] = true
	}
}

func (da *DataflowAnalyzer) MergeBranch(other *DataflowAnalyzer) {
	for name, vals := range other.defs {
		existing, exists := da.defs[name]
		if !exists {
			if vals == nil {
				da.defs[name] = nil
			} else {
				da.defs[name] = append([]string{}, vals...)
			}
			continue
		}

		// Indeterminate in either branch remains indeterminate.
		if existing == nil || vals == nil {
			da.defs[name] = nil
			continue
		}

		seen := make(map[string]struct{}, len(existing)+len(vals))
		for _, v := range existing {
			seen[v] = struct{}{}
		}
		merged := append([]string{}, existing...)
		for _, v := range vals {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			merged = append(merged, v)
		}
		da.defs[name] = merged
	}

	for name, exported := range other.exports {
		if exported {
			da.exports[name] = true
		}
	}
}

func (da *DataflowAnalyzer) Resolve(name string) []string {
	return da.defs[name]
}

func (da *DataflowAnalyzer) ResolveString(s string) (expansions []string, capped bool) {
	matches := varRefRE.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return []string{s}, false
	}

	type ref struct {
		raw  string
		name string
	}
	parts := make([]string, 0, len(matches)+1)
	refs := make([]ref, 0, len(matches))

	prev := 0
	for _, m := range matches {
		parts = append(parts, s[prev:m[0]])
		raw := s[m[0]:m[1]]
		name := ""
		if m[2] >= 0 {
			name = s[m[2]:m[3]]
		} else if m[4] >= 0 {
			name = s[m[4]:m[5]]
		}
		refs = append(refs, ref{raw: raw, name: name})
		prev = m[1]
	}
	parts = append(parts, s[prev:])

	var out []string
	var build func(i int, acc string)
	build = func(i int, acc string) {
		if len(out) >= maxExpansions {
			capped = true
			return
		}
		if i == len(refs) {
			out = append(out, acc+parts[i])
			return
		}

		acc = acc + parts[i]
		values := da.Resolve(refs[i].name)
		if values == nil || len(values) == 0 {
			build(i+1, acc+refs[i].raw)
			return
		}
		for _, v := range values {
			build(i+1, acc+v)
			if capped {
				return
			}
		}
	}
	build(0, "")
	if len(out) == 0 {
		return []string{s}, capped
	}
	return out, capped
}

func (da *DataflowAnalyzer) ExportedVars() map[string][]string {
	out := make(map[string][]string)
	for name := range da.exports {
		if vals := da.defs[name]; len(vals) > 0 {
			out[name] = append([]string{}, vals...)
		}
	}
	return out
}
