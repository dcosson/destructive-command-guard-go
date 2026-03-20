package eval

import (
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// PreFilter performs a fast keyword check before full evaluation using an
// Aho-Corasick automaton that matches all pack keywords simultaneously in a
// single O(n) pass over the input. Word-boundary post-filtering eliminates
// false positives (e.g. "git" inside "github").
type PreFilter struct {
	automaton *ahoCorasick
	keywords  []string // canonical keyword list from registry
	registry  *packs.Registry
}

// FilterResult holds the outcome of pre-filter matching.
type FilterResult struct {
	Matched  bool
	Keywords []string // matched keywords (word-boundary filtered)
}

// NewPreFilter builds a prefilter from pack registry keywords using
// Aho-Corasick for O(n) multi-pattern matching.
func NewPreFilter(registry *packs.Registry) *PreFilter {
	if registry == nil {
		return &PreFilter{}
	}
	keywords := registry.Keywords()
	ac := newAhoCorasick(keywords)
	return &PreFilter{
		automaton: ac,
		keywords:  keywords,
		registry:  registry,
	}
}

// Contains scans the command for any registered keyword, returning matched
// keywords that appear at word boundaries.
func (p *PreFilter) Contains(command string) FilterResult {
	if p == nil || p.automaton == nil || len(p.keywords) == 0 {
		return FilterResult{}
	}

	rawMatches := p.automaton.search(command)
	if len(rawMatches) == 0 {
		return FilterResult{}
	}

	// Deduplicate and word-boundary filter.
	seen := make(map[string]struct{})
	var matched []string
	for _, m := range rawMatches {
		if !isWordBoundary(command, m.Start, m.End) {
			continue
		}
		kw := p.keywords[m.PatternIdx]
		if _, ok := seen[kw]; ok {
			continue
		}
		seen[kw] = struct{}{}
		matched = append(matched, kw)
	}

	if len(matched) == 0 {
		return FilterResult{}
	}

	return FilterResult{
		Matched:  true,
		Keywords: matched,
	}
}

// MayContainDestructive is a backward-compatible wrapper around Contains.
func (p *PreFilter) MayContainDestructive(command string) bool {
	return p.Contains(command).Matched
}

// CandidatePacks returns pack IDs whose keywords matched in the command,
// filtered to only include packs from enabledPacks (if non-nil).
func (p *PreFilter) CandidatePacks(matchedKeywords []string, enabledPacks []string) []string {
	if p == nil || p.registry == nil {
		return nil
	}

	enabledSet := make(map[string]struct{})
	for _, id := range enabledPacks {
		enabledSet[id] = struct{}{}
	}

	seen := make(map[string]struct{})
	var result []string
	for _, kw := range matchedKeywords {
		for _, packID := range p.registry.PacksForKeyword(kw) {
			if _, ok := seen[packID]; ok {
				continue
			}
			seen[packID] = struct{}{}
			if enabledPacks != nil {
				if _, ok := enabledSet[packID]; !ok {
					continue
				}
			}
			result = append(result, packID)
		}
	}
	return result
}

// isWordBoundary checks that a match at [start, end) is bounded by non-word
// characters (or string boundaries).
func isWordBoundary(text string, start, end int) bool {
	if start > 0 && isWordChar(text[start-1]) {
		return false
	}
	if end < len(text) && isWordChar(text[end]) {
		return false
	}
	return true
}

// isWordChar returns true for characters considered part of a word: [a-zA-Z0-9_-].
// Hyphen is included because keywords like "redis-cli" contain hyphens.
func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-'
}
