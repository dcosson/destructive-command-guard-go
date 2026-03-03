package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func runPacksMode(args []string) error {
	fs := flag.NewFlagSet("packs", flag.ContinueOnError)
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
			fmt.Fprintf(stdout, "  %-25s %s\n", "", p.Description)
		}
		fmt.Fprintf(stdout, "  %-25s Keywords: %v | %d safe, %d destructive patterns\n", "", p.Keywords, p.SafeCount, p.DestrCount)
		fmt.Fprintln(stdout)
	}
	return nil
}
