package packs

import (
	"sort"
	"strings"
	"sync"

	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
)

// Rule is a single pack rule.
type Rule struct {
	ID           string
	Category     evalcore.RuleCategory
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
	Rules           []Rule
	HasEnvSensitive bool
}

// Registry is a read-only pack set.
type Registry struct {
	mu           sync.RWMutex
	all          []Pack
	by           map[string]Pack
	keywords     []string
	keywordIndex map[string][]string // keyword → pack IDs
}

func NewRegistry(packs ...Pack) *Registry {
	all := append([]Pack(nil), packs...)
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	by := make(map[string]Pack, len(all))
	for _, p := range all {
		p.HasEnvSensitive = hasEnvSensitive(p)
		by[p.ID] = p
	}
	r := &Registry{all: all, by: by}
	r.buildKeywordIndex()
	return r
}

func (r *Registry) buildKeywordIndex() {
	seen := make(map[string]struct{})
	r.keywordIndex = make(map[string][]string)
	for _, p := range r.all {
		for _, kw := range p.Keywords {
			kw = strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			r.keywordIndex[kw] = append(r.keywordIndex[kw], p.ID)
			seen[kw] = struct{}{}
		}
	}
	r.keywords = make([]string, 0, len(seen))
	for kw := range seen {
		r.keywords = append(r.keywords, kw)
	}
	sort.Strings(r.keywords)
}

func (r *Registry) rebuildSortedAllLocked() {
	r.all = r.all[:0]
	for _, p := range r.by {
		r.all = append(r.all, p)
	}
	sort.Slice(r.all, func(i, j int) bool { return r.all[i].ID < r.all[j].ID })
	r.buildKeywordIndex()
}

func hasEnvSensitive(p Pack) bool {
	if p.HasEnvSensitive {
		return true
	}
	for _, r := range p.Rules {
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Pack, len(r.all))
	copy(out, r.all)
	return out
}

func (r *Registry) Get(id string) (Pack, bool) {
	if r == nil {
		return Pack{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.by[id]
	return p, ok
}

// Keywords returns the deduplicated, sorted list of all pack keywords.
func (r *Registry) Keywords() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.keywords))
	copy(out, r.keywords)
	return out
}

// PacksForKeyword returns the IDs of all packs that contain the given keyword.
func (r *Registry) PacksForKeyword(keyword string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.keywordIndex[keyword]
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

// Register adds or replaces packs in the registry.
func (r *Registry) Register(packs ...Pack) {
	if r == nil || len(packs) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by == nil {
		r.by = make(map[string]Pack, len(packs))
	}
	for _, p := range packs {
		p.HasEnvSensitive = hasEnvSensitive(p)
		r.by[p.ID] = p
	}
	r.rebuildSortedAllLocked()
}

// MatchCommand preserves existing test callsites while rules are now parse-based.
func (r Rule) MatchCommand(cmd Command) bool {
	if r.Match == nil {
		return false
	}
	return r.Match.Match(cmd)
}
