package eval

// Shared pack test helpers for database pack tests.

import (
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// dbPackIDs are the 5 database pack identifiers.
var dbPackIDs = []string{
	"database.postgresql",
	"database.mysql",
	"database.sqlite",
	"database.mongodb",
	"database.redis",
}

// dbPack returns the pack with the given ID from the default registry.
func dbPack(id string) *packs.Pack {
	p, ok := packs.DefaultRegistry.Get(id)
	if !ok {
		return nil
	}
	return &p
}

// findRuleByID returns the destructive rule with the given ID, or nil.
func findRuleByID(pack *packs.Pack, id string) *packs.Rule {
	for i := range pack.Destructive {
		if pack.Destructive[i].ID == id {
			return &pack.Destructive[i]
		}
	}
	return nil
}

// matchPackDestructive evaluates a command against a pack's destructive rules.
// Returns the ID of the first matching rule, or "" if nothing matches.
func matchPackDestructive(pack *packs.Pack, cmd string) string {
	for _, rule := range pack.Destructive {
		if rule.Match != nil && rule.Match(cmd) {
			return rule.ID
		}
	}
	return ""
}
