package packs

import (
	"sort"
)

// Rule is a single pack rule.
type Rule struct {
	ID           string
	Severity     int
	Confidence   int
	Reason       string
	Remediation  string
	EnvSensitive bool
	Match        MatchFunc
}

// Pack groups related rules.
type Pack struct {
	ID              string
	Name            string
	Description     string
	Keywords        []string
	Safe            []Rule
	Destructive     []Rule
	HasEnvSensitive bool
}

// Registry is a read-only pack set.
type Registry struct {
	all []Pack
	by  map[string]Pack
}

func NewRegistry(packs ...Pack) *Registry {
	all := append([]Pack(nil), packs...)
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	by := make(map[string]Pack, len(all))
	for _, p := range all {
		p.HasEnvSensitive = hasEnvSensitive(p)
		by[p.ID] = p
	}
	return &Registry{all: all, by: by}
}

func hasEnvSensitive(p Pack) bool {
	if p.HasEnvSensitive {
		return true
	}
	for _, r := range p.Destructive {
		if r.EnvSensitive {
			return true
		}
	}
	return false
}

func (r *Registry) All() []Pack {
	if r == nil {
		return nil
	}
	out := make([]Pack, len(r.all))
	copy(out, r.all)
	return out
}

func (r *Registry) Get(id string) (Pack, bool) {
	if r == nil {
		return Pack{}, false
	}
	p, ok := r.by[id]
	return p, ok
}

// MatchCommand preserves existing test callsites while rules are now parse-based.
func (r Rule) MatchCommand(cmd Command) bool {
	if r.Match == nil {
		return false
	}
	return r.Match.Match(cmd)
}
