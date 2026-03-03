package testharness

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

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

			for _, m := range report.Mutations {
				if !m.Killed {
					t.Errorf("SURVIVED: %s.%s [%s] %s", m.Pack, m.Pattern, m.Operator, m.Detail)
				}
			}
			if report.Survived != 0 {
				t.Fatalf("pack %s: %d mutations survived", pack.ID, report.Survived)
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
