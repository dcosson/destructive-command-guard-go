package e2etest

// Shared pack test helpers for database pack tests.

import (
	"context"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
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
	parser := parse.NewBashParser()
	parsed := parser.ParseAndExtract(context.Background(), cmd, 0)
	for _, extracted := range parsed.Commands {
		pc := packs.Command{
			Name:    extracted.Name,
			Args:    append([]string{}, extracted.Args...),
			RawArgs: append([]string{}, extracted.RawArgs...),
			Flags:   extracted.Flags,
			RawText: extracted.RawText,
		}
		for _, rule := range pack.Destructive {
			if rule.Match != nil && rule.Match.Match(pc) {
				return rule.ID
			}
		}
	}
	return ""
}

// matchRuleCommand is a convenience for tests still asserting individual rules.
func matchRuleCommand(rule any, cmd string) bool {
	var matcher packs.MatchFunc
	switch r := rule.(type) {
	case *packs.Rule:
		if r == nil {
			return false
		}
		matcher = r.Match
	case packs.Rule:
		matcher = r.Match
	default:
		return false
	}
	if matcher == nil {
		return false
	}
	parser := parse.NewBashParser()
	parsed := parser.ParseAndExtract(context.Background(), cmd, 0)
	for _, extracted := range parsed.Commands {
		pc := packs.Command{
			Name:    extracted.Name,
			Args:    append([]string{}, extracted.Args...),
			RawArgs: append([]string{}, extracted.RawArgs...),
			Flags:   extracted.Flags,
			RawText: extracted.RawText,
		}
		if matcher.Match(pc) {
			return true
		}
	}
	return false
}
