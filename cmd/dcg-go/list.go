package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"sort"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func runListMode(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dcg-go list {packs,rules} [--json]")
	}

	switch args[0] {
	case "packs":
		return runListPacks(args[1:])
	case "rules":
		return runListRules(args[1:])
	default:
		return fmt.Errorf("unknown list subcommand: %s (valid: packs, rules)", args[0])
	}
}

func runListPacks(args []string) error {
	fs := flag.NewFlagSet("list packs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	packs := guard.Packs()
	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(packs)
	}

	fmt.Fprintf(stdout, "Registered packs (%d):\n\n", len(packs))
	for _, p := range packs {
		fmt.Fprintf(stdout, "  %-25s %s\n", p.ID, p.Name)
		if p.Description != "" {
			fmt.Fprintf(stdout, "  %-25s %s\n", "", wrapLine(p.Description, contentCol, wrapWidth))
		}
		fmt.Fprintf(stdout, "  %-25s %s\n", "", formatKeywords(p.Keywords))
		fmt.Fprintf(stdout, "  %-25s %d destructive, %d privacy, %d both\n",
			"", p.DestructiveCount, p.PrivacyCount, p.BothCount)
		fmt.Fprintln(stdout)
	}
	return nil
}

func runListRules(args []string) error {
	fs := flag.NewFlagSet("list rules", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "Output as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rules := guard.Rules()
	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rules)
	}

	var destructive, privacy, both []guard.RuleInfo
	for _, r := range rules {
		switch r.Category {
		case guard.CategoryBoth:
			both = append(both, r)
		case guard.CategoryPrivacy:
			privacy = append(privacy, r)
		default:
			destructive = append(destructive, r)
		}
	}

	sortRules := func(rs []guard.RuleInfo) {
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].PackID != rs[j].PackID {
				return rs[i].PackID < rs[j].PackID
			}
			return rs[i].ID < rs[j].ID
		})
	}
	sortRules(destructive)
	sortRules(privacy)
	sortRules(both)

	printGroup := func(label string, rs []guard.RuleInfo) {
		fmt.Fprintf(stdout, "%s (%d):\n", label, len(rs))
		for _, r := range rs {
			prefix := fmt.Sprintf("  %-40s (%s) ", r.ID, r.PackID)
			fmt.Fprintf(stdout, "%s%s\n", prefix, wrapLine(r.Reason, len(prefix), wrapWidth))
		}
		fmt.Fprintln(stdout)
	}

	printGroup("Destructive", destructive)
	printGroup("Privacy", privacy)
	printGroup("Both", both)
	return nil
}
