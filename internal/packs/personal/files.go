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
	prefix   string // rule ID prefix, e.g. "desktop"
	name     string // human name, e.g. "Desktop"
	pattern  string // regex fragment after home prefix
	keywords []string
}

var personalDirs = []personalDir{
	{"desktop", "Desktop", "Desktop", []string{"Desktop"}},
	{"documents", "Documents", "Documents", []string{"Documents"}},
	{"downloads", "Downloads", "Downloads", []string{"Downloads"}},
	{"music", "Music", "Music", []string{"Music"}},
	{"pictures", "Pictures", "Pictures", []string{"Pictures"}},
	{"videos", "Videos", "Videos", []string{"Videos"}},
}

func filesPack() packs.Pack {
	var keywords []string
	var rules []packs.Rule

	for _, d := range personalDirs {
		keywords = append(keywords, d.keywords...)
		re := regexp.MustCompile(homePrefix + `/` + d.pattern + `(?:/|$)`)
		rules = append(rules, personalDirRules(d.prefix, d.name, re)...)
	}

	return packs.Pack{
		ID:          "personal.files",
		Name:        "Personal Files",
		Description: "Detects commands accessing personal directories (Desktop, Documents, Downloads, Music, Pictures, Videos)",
		Keywords:    keywords,
		Rules:       rules,
	}
}

func personalDirRules(prefix, name string, re *regexp.Regexp) []packs.Rule {
	return []packs.Rule{
		{ID: prefix + "-delete", Severity: sevCritical, Confidence: confHigh,
			Reason:      fmt.Sprintf("Command removes content in %s", name),
			Remediation: "Delete explicit files instead of deleting personal directories",
			Match: packs.And(
				packs.Or(packs.Name("rm"), packs.Name("shred"), packs.Name("srm"), packs.Name("unlink")),
				packs.ArgContentRegex(re.String()),
			)},
		{ID: prefix + "-overwrite", Severity: sevHigh, Confidence: confHigh,
			Reason:      fmt.Sprintf("File operation may overwrite files in %s", name),
			Remediation: "Write to a new output path instead of overwriting existing files",
			Match: packs.And(
				packs.Or(packs.Name("mv"), packs.Name("cp")),
				packs.ArgContentRegex(re.String()),
				packs.Not(packs.Or(packs.Flags("-n"), packs.Flags("--no-clobber"))),
			)},
		{ID: prefix + "-modify", Severity: sevHigh, Confidence: confMedium,
			Reason:      fmt.Sprintf("Command modifies attributes or content in %s", name),
			Remediation: "Apply changes to specific files only",
			Match: packs.And(
				packs.Or(packs.Name("chmod"), packs.Name("chown"), packs.Name("chgrp"), packs.Name("truncate")),
				packs.ArgContentRegex(re.String()),
			)},
		{ID: prefix + "-write", Severity: sevHigh, Confidence: confMedium,
			Reason:      fmt.Sprintf("Command writes new content into %s", name),
			Remediation: "Write to a temporary workspace path instead",
			Match: packs.And(
				packs.Or(packs.Name("sed"), packs.Name("tee"), packs.Name("dd")),
				packs.ArgContentRegex(re.String()),
			)},
		{ID: prefix + "-access", Category: evalcore.CategoryPrivacy, Severity: sevMedium, Confidence: confMedium,
			Reason:      fmt.Sprintf("Command reads files in %s", name),
			Remediation: "Use project-local files instead of personal directories",
			Match: packs.And(
				packs.AnyName(),
				packs.ArgContentRegex(re.String()),
			)},
	}
}
