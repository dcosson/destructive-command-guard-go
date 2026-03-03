package testharness

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func TestPropertyMutationOperatorsDiffer(t *testing.T) {
	corpus := loadMutationCorpus()
	all := packs.DefaultRegistry.All()
	if len(all) == 0 {
		t.Fatal("no packs registered")
	}

	sampled := 0
	for _, pk := range all {
		if len(pk.Destructive) == 0 {
			continue
		}
		for _, rule := range pk.Destructive {
			hit, miss := selectProbes(pk.ID, rule.ID, corpus)
			if hit == "" {
				continue
			}
			for _, m := range generateMutations() {
				// Metadata mutations intentionally preserve matcher behavior.
				if m.category == "metadata" {
					continue
				}
				// Known equivalent for DB packs: RemoveFlag does not change SQL/content-based matching.
				if isEquivalentMutation(pk.ID, m.operator) {
					continue
				}

				mutated := m.apply(rule)
				if !mutationDiffersOnCorpus(rule, mutated, corpus) {
					killed, killedBy := isKilled(rule, mutated, hit, miss)
					if !killed {
						t.Fatalf("mutation produced no observable behavior change: pack=%s rule=%s op=%s detail=%s probe=%s",
							pk.ID, rule.ID, m.operator, m.detail, killedBy)
					}
				}
			}
			sampled++
			if sampled >= 3 {
				return
			}
		}
	}

	if sampled == 0 {
		t.Skip("no destructive rules with probes found")
	}
}

func TestFaultMutationHarnessIdentityMutation(t *testing.T) {
	pk, ok := findPack("core.git")
	if !ok || len(pk.Destructive) == 0 {
		t.Skip("core.git destructive rules unavailable")
	}
	rule := pk.Destructive[0]
	hit, miss := selectProbes(pk.ID, rule.ID, loadMutationCorpus())
	if hit == "" {
		t.Skip("no hit probe available for selected rule")
	}

	identity := rule
	killed, killedBy := isKilled(rule, identity, hit, miss)
	if killed {
		t.Fatalf("identity mutation should not be killed (killed_by=%s)", killedBy)
	}
}

func TestDeterministicKnownMutationKillCoreGit(t *testing.T) {
	pk, ok := findPack("core.git")
	if !ok {
		t.Skip("core.git pack not registered")
	}
	r, ok := findRule(pk, "git-push-force")
	if !ok {
		t.Skip("core.git/git-push-force rule not registered")
	}
	hit, miss := selectProbes(pk.ID, r.ID, loadMutationCorpus())
	if hit == "" {
		t.Skip("no hit probe available for git-push-force")
	}

	var removeCondition mutation
	found := false
	for _, m := range generateMutations() {
		if m.operator == "RemoveCondition" {
			removeCondition = m
			found = true
			break
		}
	}
	if !found {
		t.Fatal("RemoveCondition mutation operator missing")
	}

	killed, killedBy := isKilled(r, removeCondition.apply(r), hit, miss)
	if !killed {
		t.Fatalf("expected RemoveCondition mutation to be killed for core.git/git-push-force")
	}
	if killedBy == "" {
		t.Fatalf("expected non-empty killedBy for known mutation kill")
	}
}

func mutationDiffersOnCorpus(original, mutated packs.Rule, corpus []string) bool {
	for _, cmd := range corpus {
		if safeMatch(original.Match, cmd) != safeMatch(mutated.Match, cmd) {
			return true
		}
	}
	return false
}

