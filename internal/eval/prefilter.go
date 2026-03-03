package eval

import (
	"strings"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// PreFilter performs a fast keyword check before full evaluation.
type PreFilter struct {
	keywords []string
}

// NewPreFilter builds a prefilter from pack registry keywords.
func NewPreFilter(registry *packs.Registry) *PreFilter {
	if registry == nil {
		return &PreFilter{}
	}
	set := make(map[string]struct{})
	for _, p := range registry.All() {
		for _, kw := range p.Keywords {
			kw = strings.TrimSpace(strings.ToLower(kw))
			if kw == "" {
				continue
			}
			set[kw] = struct{}{}
		}
	}
	keywords := make([]string, 0, len(set))
	for kw := range set {
		keywords = append(keywords, kw)
	}
	return &PreFilter{keywords: keywords}
}

// MayContainDestructive reports if command likely contains destructive intent.
func (p *PreFilter) MayContainDestructive(command string) bool {
	if p == nil || len(p.keywords) == 0 {
		return false
	}
	cmd := strings.ToLower(command)
	for _, kw := range p.keywords {
		if strings.Contains(cmd, kw) {
			return true
		}
	}
	return false
}
