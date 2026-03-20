package macos

import (
	"regexp"

	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

var (
	messagesDbRe       = regexp.MustCompile(`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users)/[^/]+)/Library/Messages/`)
	macosPrivateDataRe = regexp.MustCompile(
		`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users)/[^/]+)/` +
			`(?:Library/(?:Messages/|Mail/|Safari/|Application Support/AddressBook/|` +
			`Group Containers/group\.com\.apple\.notes/|Calendars/|Mobile Documents/)|Pictures/Photos Library\.photoslibrary/)`,
	)
	icloudRe = regexp.MustCompile(`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users)/[^/]+)/Library/Mobile Documents(?:/|$)`)
)

func privacyPack() packs.Pack {
	return packs.Pack{
		ID:          "macos.privacy",
		Name:        "macOS Privacy",
		Description: "Detects access to macOS private data (messages, email, browsing history, keychain)",
		Keywords:    []string{"security", "mdfind", "sqlite3", "Messages", "AddressBook", "apple.notes", "Safari", "Mail", "Calendars", "Photos Library", "Mobile Documents"},
		Safe: []packs.Rule{
			{ID: "security-find-cert", Match: packs.And(packs.Name("security"), packs.Or(packs.ArgAt(0, "find-certificate"), packs.ArgAt(0, "verify-cert"), packs.ArgAt(0, "cms")))},
		},
		Rules: []packs.Rule{
			{ID: "keychain-read-password", Category: evalcore.CategoryPrivacy, Severity: sevCritical, Confidence: confHigh, Reason: "Reading keychain passwords exposes credential secrets", Remediation: "Use non-secret metadata lookups instead of password retrieval", Match: packs.And(packs.Name("security"), packs.Or(packs.ArgAt(0, "find-generic-password"), packs.ArgAt(0, "find-internet-password")))},
			{ID: "keychain-dump", Category: evalcore.CategoryPrivacy, Severity: sevCritical, Confidence: confHigh, Reason: "Dumping/exporting keychain exposes stored credentials", Remediation: "Do not dump or export keychain", Match: packs.And(packs.Name("security"), packs.Or(packs.ArgAt(0, "dump-keychain"), packs.ArgAt(0, "export")))},
			{ID: "messages-db-access", Category: evalcore.CategoryPrivacy, Severity: sevHigh, Confidence: confHigh, Reason: "Command accesses iMessage database", Remediation: "Do not access personal messages", Match: packs.And(packs.AnyName(), packs.ArgContentRegex(messagesDbRe.String()))},
			{ID: "private-data-access", Category: evalcore.CategoryPrivacy, Severity: sevHigh, Confidence: confHigh, Reason: "Command accesses private data stores such as mail, contacts, notes, or history", Remediation: "Use project-scoped files instead of private data stores", Match: packs.And(packs.AnyName(), packs.ArgContentRegex(macosPrivateDataRe.String()), packs.Not(packs.ArgContentRegex(messagesDbRe.String())))},
			{ID: "spotlight-search", Category: evalcore.CategoryPrivacy, Severity: sevMedium, Confidence: confLow, Reason: "mdfind can enumerate personal files by content", Remediation: "Use path-scoped search commands instead of global indexing search", Match: packs.Name("mdfind")},
			{ID: "icloud-delete", Severity: sevCritical, Confidence: confHigh, Reason: "Command removes content in iCloud Drive", Remediation: "Delete explicit files instead of deleting iCloud directories", Match: packs.And(
				packs.Or(packs.Name("rm"), packs.Name("shred"), packs.Name("srm"), packs.Name("unlink")),
				packs.ArgContentRegex(icloudRe.String()),
			)},
			{ID: "icloud-overwrite", Severity: sevHigh, Confidence: confHigh, Reason: "File operation may overwrite files in iCloud Drive", Remediation: "Write to a new output path instead of overwriting existing files", Match: packs.And(
				packs.Or(packs.Name("mv"), packs.Name("cp")),
				packs.ArgContentRegex(icloudRe.String()),
				packs.Not(packs.Or(packs.Flags("-n"), packs.Flags("--no-clobber"))),
			)},
			{ID: "icloud-access", Category: evalcore.CategoryPrivacy, Severity: sevMedium, Confidence: confMedium, Reason: "Command reads files in iCloud Drive", Remediation: "Use project-local files instead of iCloud directories", Match: packs.And(
				packs.AnyName(),
				packs.ArgContentRegex(icloudRe.String()),
			)},
		},
	}
}
