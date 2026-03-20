package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

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
		fmt.Fprintf(stdout, "  %-25s %s\n", "", formatKeywords(p.Keywords))
		fmt.Fprintf(stdout, "  %-25s %d safe, %d destructive patterns\n", "", p.SafeCount, p.DestrCount)
		fmt.Fprintln(stdout)
	}
	return nil
}

// formatKeywords wraps a keyword list at ~80 columns with proper indentation.
func formatKeywords(keywords []string) string {
	const (
		maxWidth = 80
		prefix   = "Keywords: "
		// 2 leading spaces + 25 padded field = 27 chars before content
		indent = "                            " // 28 spaces to align with prefix
	)

	if len(keywords) == 0 {
		return prefix + "(none)"
	}

	var b strings.Builder
	b.WriteString(prefix)
	lineLen := 27 + len(prefix) // account for the leading column padding

	for i, kw := range keywords {
		sep := ", "
		if i == 0 {
			sep = ""
		}
		addition := sep + kw
		if i > 0 && lineLen+len(addition) > maxWidth {
			b.WriteString(",\n")
			b.WriteString(indent)
			b.WriteString(strings.Repeat(" ", len(prefix)))
			b.WriteString(kw)
			lineLen = len(indent) + len(prefix) + len(kw)
		} else {
			b.WriteString(addition)
			lineLen += len(addition)
		}
	}
	return b.String()
}
