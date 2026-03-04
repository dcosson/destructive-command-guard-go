package macos

import (
	"regexp"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

var (
	messagesDbRe       = regexp.MustCompile(`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users)/[^/]+)/Library/Messages/`)
	macosPrivateDataRe = regexp.MustCompile(
		`(?:~|(?:\$HOME|\$\{HOME\})|/(?:Users)/[^/]+)/` +
			`(?:Library/(?:Messages/|Mail/|Safari/|Application Support/AddressBook/|` +
			`Group Containers/group\.com\.apple\.notes/|Calendars/)|Pictures/Photos Library\.photoslibrary/)`,
	)
)

func privacyPack() packs.Pack {
	return packs.Pack{
		ID:          "macos.privacy",
		Name:        "macOS Privacy",
		Description: "Detects access to macOS private data (messages, email, browsing history, keychain)",
		Keywords:    []string{"security", "mdfind", "sqlite3", "Messages", "AddressBook", "apple.notes", "Safari", "Mail", "Calendars", "Photos Library"},
		Safe: []packs.Rule{
			{ID: "security-find-cert", Match: packs.And(packs.Name("security"), packs.Or(packs.ArgAt(0, "find-certificate"), packs.ArgAt(0, "verify-cert"), packs.ArgAt(0, "cms")))},
		},
		Destructive: []packs.Rule{
			{ID: "keychain-read-password", Severity: sevCritical, Confidence: confHigh, Reason: "Reading passwords from macOS Keychain", Remediation: "Do not access keychain passwords directly", Match: packs.And(packs.Name("security"), packs.Or(packs.ArgAt(0, "find-generic-password"), packs.ArgAt(0, "find-internet-password")))},
			{ID: "keychain-dump", Severity: sevCritical, Confidence: confHigh, Reason: "Dumping/exporting keychain exposes stored credentials", Remediation: "Do not dump or export keychain", Match: packs.And(packs.Name("security"), packs.Or(packs.ArgAt(0, "dump-keychain"), packs.ArgAt(0, "export")))},
			{ID: "messages-db-access", Severity: sevHigh, Confidence: confHigh, Reason: "Command accesses iMessage database", Remediation: "Do not access personal messages", Match: packs.And(packs.AnyName(), packs.ArgContentRegex(messagesDbRe.String()))},
			{ID: "private-data-access", Severity: sevHigh, Confidence: confHigh, Reason: "Command accesses private data stores (mail, contacts, notes, history)", Remediation: "Verify personal data access is intentional", Match: packs.And(packs.AnyName(), packs.ArgContentRegex(macosPrivateDataRe.String()), packs.Not(packs.ArgContentRegex(messagesDbRe.String())))},
			{ID: "spotlight-search", Severity: sevMedium, Confidence: confLow, Reason: "mdfind can search personal files by content", Remediation: "Prefer scoped search commands where possible", Match: packs.Name("mdfind")},
		},
	}
}
