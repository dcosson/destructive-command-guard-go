package personal

import (
	"fmt"
	"regexp"

	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

const (
	sevLow      = 1
	sevMedium   = 2
	sevHigh     = 3
	sevCritical = 4

	confLow    = 0
	confMedium = 1
	confHigh   = 2
)

// homePrefix matches ~, $HOME, ${HOME}, /Users/<user>, /home/<user>, /root.
const homePrefix = `(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users|home)/[^/]+|/root)`

type personalDir struct {
	prefix         string
	name           string
	pattern        string
	keywords       []string
	deleteSeverity int // severity for delete/overwrite rules
	accessSeverity int // severity for the read-access (privacy) rule
}

var personalDirs = []personalDir{
	{"desktop", "Desktop", "Desktop", []string{"Desktop"}, sevHigh, sevMedium},
	{"documents", "Documents", "Documents", []string{"Documents"}, sevHigh, sevMedium},
	{"downloads", "Downloads", "Downloads", []string{"Downloads"}, sevHigh, sevLow},
	{"music", "Music", "Music", []string{"Music"}, sevHigh, sevLow},
	{"pictures", "Pictures", "Pictures", []string{"Pictures"}, sevHigh, sevMedium},
	{"videos", "Videos", "Videos", []string{"Videos"}, sevHigh, sevLow},
}

func filesPack() packs.Pack {
	var keywords []string
	var rules []packs.Rule

	for _, d := range personalDirs {
		keywords = append(keywords, d.keywords...)
		re := regexp.MustCompile(homePrefix + `/` + d.pattern + `(?:/|$)`)
		rules = append(rules, personalDirRules(d, re)...)
	}

	return packs.Pack{
		ID:          "personal.files",
		Name:        "Personal Files",
		Description: "Detects commands accessing personal directories (Desktop, Documents, Downloads, Music, Pictures, Videos)",
		Keywords:    keywords,
		Rules:       rules,
	}
}

func personalDirRules(d personalDir, re *regexp.Regexp) []packs.Rule {
	return []packs.Rule{
		{ID: d.prefix + "-delete", Severity: d.deleteSeverity, Confidence: confHigh,
			Reason:      fmt.Sprintf("Command removes content in %s", d.name),
			Remediation: "Delete explicit files instead of deleting personal directories",
			Match: packs.And(
				packs.Or(packs.Name("rm"), packs.Name("shred"), packs.Name("srm"), packs.Name("unlink")),
				packs.ArgContentRegex(re.String()),
			)},
		{ID: d.prefix + "-overwrite", Severity: d.deleteSeverity, Confidence: confHigh,
			Reason:      fmt.Sprintf("File operation may overwrite files in %s", d.name),
			Remediation: "Write to a new output path instead of overwriting existing files",
			Match: packs.And(
				packs.Or(packs.Name("mv"), packs.Name("cp")),
				packs.ArgContentRegex(re.String()),
				packs.Not(packs.Or(packs.Flags("-n"), packs.Flags("--no-clobber"))),
			)},
		{ID: d.prefix + "-modify", Severity: d.deleteSeverity, Confidence: confMedium,
			Reason:      fmt.Sprintf("Command modifies attributes or content in %s", d.name),
			Remediation: "Apply changes to specific files only",
			Match: packs.And(
				packs.Or(packs.Name("chmod"), packs.Name("chown"), packs.Name("chgrp"), packs.Name("truncate")),
				packs.ArgContentRegex(re.String()),
			)},
		{ID: d.prefix + "-write", Severity: d.deleteSeverity, Confidence: confMedium,
			Reason:      fmt.Sprintf("Command writes new content into %s", d.name),
			Remediation: "Write to a temporary workspace path instead",
			Match: packs.And(
				packs.Or(packs.Name("sed"), packs.Name("tee"), packs.Name("dd")),
				packs.ArgContentRegex(re.String()),
			)},
		{ID: d.prefix + "-access", Category: evalcore.CategoryPrivacy, Severity: d.accessSeverity, Confidence: confMedium,
			Reason:      fmt.Sprintf("Command reads files in %s", d.name),
			Remediation: "Use project-local files instead of personal directories",
			Match: packs.And(
				packs.AnyName(),
				packs.ArgContentRegex(re.String()),
			)},
	}
}
