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
			fmt.Fprintf(stdout, "  %-25s %s\n", "", wrapLine(p.Description, contentCol, wrapWidth))
		}
		fmt.Fprintf(stdout, "  %-25s %s\n", "", formatKeywords(p.Keywords))
		fmt.Fprintf(stdout, "  %-25s %d destructive, %d privacy, %d both\n", "", p.DestructiveCount, p.PrivacyCount, p.BothCount)
		fmt.Fprintln(stdout)
	}
	return nil
}

const (
	// Column where content starts (2 leading spaces + 25 padded field + 1).
	contentCol = 28
	// Target max line width for wrapping.
	wrapWidth = 80
	// Indentation string to align continuation lines with content.
	contIndent = "                            " // 28 spaces
)

// wrapLine wraps prose text at ~wrapWidth columns, breaking on spaces.
// startCol is the column offset where the text begins on its first line.
func wrapLine(text string, startCol, maxWidth int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var b strings.Builder
	lineLen := startCol

	for i, w := range words {
		if i == 0 {
			b.WriteString(w)
			lineLen += len(w)
			continue
		}
		if lineLen+1+len(w) > maxWidth {
			b.WriteString("\n")
			b.WriteString(contIndent)
			b.WriteString(w)
			lineLen = contentCol + len(w)
		} else {
			b.WriteString(" ")
			b.WriteString(w)
			lineLen += 1 + len(w)
		}
	}
	return b.String()
}

// formatKeywords wraps a keyword list at ~wrapWidth columns with proper indentation.
func formatKeywords(keywords []string) string {
	prefix := "Keywords: "

	if len(keywords) == 0 {
		return prefix + "(none)"
	}

	var b strings.Builder
	b.WriteString(prefix)
	lineLen := contentCol + len(prefix)

	for i, kw := range keywords {
		sep := ", "
		if i == 0 {
			sep = ""
		}
		addition := sep + kw
		if i > 0 && lineLen+len(addition) > wrapWidth {
			b.WriteString(",\n")
			b.WriteString(contIndent)
			b.WriteString(strings.Repeat(" ", len(prefix)))
			b.WriteString(kw)
			lineLen = contentCol + len(prefix) + len(kw)
		} else {
			b.WriteString(addition)
			lineLen += len(addition)
		}
	}
	return b.String()
}
