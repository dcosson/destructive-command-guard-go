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
		Destructive: []packs.Rule{
			{ID: "personal-files-delete", Severity: sevCritical, Confidence: confHigh, Reason: "Destructive command targets a personal file directory", Remediation: "Verify this deletion is intentional", Match: packs.And(
				packs.Or(packs.Name("rm"), packs.Name("shred"), packs.Name("srm"), packs.Name("unlink")),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
			{ID: "personal-files-overwrite", Severity: sevHigh, Confidence: confHigh, Reason: "File operation targets a personal directory and may overwrite files", Remediation: "Use no-clobber flags or verify paths", Match: packs.And(
				packs.Or(packs.Name("mv"), packs.Name("cp")),
				packs.ArgContentRegex(personalPathRe.String()),
				packs.Not(packs.Or(packs.Flags("-n"), packs.Flags("--no-clobber"))),
			)},
			{ID: "personal-files-modify", Severity: sevHigh, Confidence: confMedium, Reason: "Command modifies personal file attributes or content", Remediation: "Verify this modification is intentional", Match: packs.And(
				packs.Or(packs.Name("chmod"), packs.Name("chown"), packs.Name("chgrp"), packs.Name("truncate")),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
			{ID: "personal-files-write", Severity: sevHigh, Confidence: confMedium, Reason: "Command writes to a personal file directory", Remediation: "Verify this write target is correct", Match: packs.And(
				packs.Or(packs.Name("sed"), packs.Name("tee"), packs.Name("dd")),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
			{ID: "personal-files-access", Severity: sevMedium, Confidence: confMedium, Reason: "Command accesses a personal file directory", Remediation: "Verify this access is intentional", Match: packs.And(
				packs.AnyName(),
				packs.ArgContentRegex(personalPathRe.String()),
			)},
		},
	}
}
