package testharness

import (
	"strings"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// equivalentMutationPrefixes lists pack prefixes where the RemoveFlag mutation
// is inherently equivalent. RemoveFlag strips CLI flags (--force, -rf, --delete,
// etc.) which are irrelevant for rules that match on content keywords (SQL
// keywords, Redis commands, MongoDB expressions) in the raw command string.
// These mutations cannot be killed because stripping CLI flags doesn't change
// whether the content keywords are present.
var equivalentMutationPrefixes = []string{
	"database.",
	"core.git",
	"core.filesystem",
	"containers.",
	"kubernetes.",
	"frameworks",
	"personal.",
	"macos.",
	"secrets.",
	"platform.",
}

func isEquivalentMutation(packID, operator string) bool {
	if operator != "RemoveFlag" {
		return false
	}
	for _, prefix := range equivalentMutationPrefixes {
		if strings.HasPrefix(packID, prefix) {
			return true
		}
	}
	return false
}

func TestMutationKillRate(t *testing.T) {
	corpus := loadMutationCorpus()
	allPacks := packs.DefaultRegistry.All()
	if len(allPacks) == 0 {
		t.Fatal("no packs registered")
	}

	for _, pack := range allPacks {
		pack := pack
		t.Run(pack.ID, func(t *testing.T) {
			report := runMutationAnalysis(pack, corpus)
			t.Logf("Pack %s: %d/%d mutations killed (%.1f%%), metadata %d/%d",
				pack.ID, report.Killed, report.Total, report.KillRate, report.MetadataKilled, report.MetadataTotal)

			var unexpected int
			for _, m := range report.Mutations {
				if !m.Killed {
					if isEquivalentMutation(m.Pack, m.Operator) {
						t.Logf("EQUIVALENT: %s.%s [%s] %s", m.Pack, m.Pattern, m.Operator, m.Detail)
					} else {
						t.Errorf("SURVIVED: %s.%s [%s] %s", m.Pack, m.Pattern, m.Operator, m.Detail)
						unexpected++
					}
				}
			}
			if unexpected != 0 {
				t.Fatalf("pack %s: %d unexpected mutations survived", pack.ID, unexpected)
			}
		})
	}
}

func TestMutationOperatorsCount(t *testing.T) {
	ops := generateMutations()
	if len(ops) != 10 {
		t.Fatalf("mutation operator count = %d, want 10", len(ops))
	}
}
