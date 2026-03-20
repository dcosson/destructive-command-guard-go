package personal

import (
	"regexp"

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

var personalPathRe = regexp.MustCompile(
	`(?:` +
		`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users|home)/[^/]+|/root)` +
		`/(?:Desktop|Documents|Downloads|Pictures|Music|Videos)(?:/|$)` +
		`|` +
		`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users|home)/[^/]+|/root)` +
		`/Library/Mobile Documents(?:/|$)` +
		`)`,
)

func filesPack() packs.Pack {
	return packs.Pack{
		ID:          "personal.files",
		Name:        "Personal Files",
		Description: "Detects commands accessing personal file directories (Desktop, Documents, Downloads, etc.)",
		Keywords: []string{
			"Desktop", "Documents", "Downloads", "Pictures", "Music", "Videos", "Mobile Documents",
		},
		Rules: []packs.Rule{
			{ID: "personal-files-delete", Severity: sevCritical, Confidence: confHigh, Reason: "Command removes content in a personal file directory", Remediation: "Delete explicit files instead of deleting personal directories", Match: packs.And(
				packs.Or(packs.Name("rm"), packs.Name("shred"), packs.Name("srm"), packs.Name("unlink")),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
			{ID: "personal-files-overwrite", Severity: sevHigh, Confidence: confHigh, Reason: "File operation may overwrite files in a personal directory", Remediation: "Write to a new output path instead of overwriting existing files", Match: packs.And(
				packs.Or(packs.Name("mv"), packs.Name("cp")),
				packs.ArgContentRegex(personalPathRe.String()),
				packs.Not(packs.Or(packs.Flags("-n"), packs.Flags("--no-clobber"))),
			)},
			{ID: "personal-files-modify", Severity: sevHigh, Confidence: confMedium, Reason: "Command modifies attributes or content in a personal directory", Remediation: "Apply changes to specific files only", Match: packs.And(
				packs.Or(packs.Name("chmod"), packs.Name("chown"), packs.Name("chgrp"), packs.Name("truncate")),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
			{ID: "personal-files-write", Severity: sevHigh, Confidence: confMedium, Reason: "Command writes new content into a personal directory", Remediation: "Write to a temporary workspace path instead", Match: packs.And(
				packs.Or(packs.Name("sed"), packs.Name("tee"), packs.Name("dd")),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
			{ID: "personal-files-access", Severity: sevMedium, Confidence: confMedium, Reason: "Command reads files in a personal directory", Remediation: "Use project-local files instead of personal directories", Match: packs.And(
				packs.AnyName(),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
		},
	}
}
