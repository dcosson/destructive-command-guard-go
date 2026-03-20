package core

import (
	"regexp"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

func init() {
	packs.DefaultRegistry.Register(FilesystemPack())
}

var (
	reChmod777    = regexp.MustCompile(`(?i)\bchmod\b.*\b777\b`)
	reChmod000    = regexp.MustCompile(`(?i)\bchmod\b.*\b000\b`)
	reTruncate0   = regexp.MustCompile(`(?i)\btruncate\b.*(?:--size(?:=|\s+)0|-s\s*0)(?:\s|$)`)
	reTruncateS0  = regexp.MustCompile(`(?i)\btruncate\b.*\b-s\b`)
	reTruncateSz0 = regexp.MustCompile(`(?i)\btruncate\b.*\b--size(?:=|\s+)0`)
)

func FilesystemPack() packs.Pack {
	return packs.Pack{
		ID:          "core.filesystem",
		Name:        "Filesystem",
		Description: "Filesystem destructive operations",
		Keywords:    []string{"rm", "dd", "shred", "chmod", "chown", "mkfs", "mv", "truncate"},
		Safe: []packs.Rule{
			{
				ID: "rm-single-safe",
				Match: packs.And(
					packs.Name("rm"),
					packs.Not(packs.Or(
						packs.Flags("-r"),
						packs.Flags("-R"),
						packs.Flags("--recursive"),
						packs.Flags("-rf"),
						packs.Flags("-fr"),
					)),
				),
			},
			{
				ID: "chmod-single-safe",
				Match: packs.And(
					packs.Name("chmod"),
					packs.Not(packs.Or(packs.Flags("-R"), packs.Flags("--recursive"))),
					packs.Not(packs.Or(
						packs.RawTextRegex(reChmod777),
						packs.RawTextRegex(reChmod000),
					)),
				),
			},
			{
				ID: "chown-single-safe",
				Match: packs.And(
					packs.Name("chown"),
					packs.Not(packs.Or(packs.Flags("-R"), packs.Flags("--recursive"))),
				),
			},
			{
				ID: "mv-safe",
				Match: packs.And(
					packs.Name("mv"),
					packs.Not(packs.Arg("/dev/null")),
				),
			},
		},
		Rules: []packs.Rule{
			{
				ID: "rm-rf-root",
				Match: packs.And(
					packs.Name("rm"),
					packs.Or(
						packs.Flags("-rf"),
						packs.Flags("-fr"),
						packs.And(
							packs.Or(packs.Flags("-r"), packs.Flags("-R"), packs.Flags("--recursive")),
							packs.Or(packs.Flags("-f"), packs.Flags("--force")),
						),
					),
					packs.Or(packs.Arg("/"), packs.Arg("/*"), packs.Arg("/..")),
				),
				Severity:    sevCritical,
				Confidence:  confHigh,
				Reason:      "rm -rf / recursively deletes the entire filesystem",
				Remediation: "Delete specific files or directories individually rather than using recursive force delete",
			},
			{
				ID: "mkfs-any",
				Match: packs.Or(
					packs.Name("mkfs"),
					packs.Name("mkfs.ext4"),
					packs.Name("mkfs.ext3"),
					packs.Name("mkfs.ext2"),
					packs.Name("mkfs.xfs"),
					packs.Name("mkfs.btrfs"),
					packs.Name("mkfs.ntfs"),
					packs.Name("mkfs.vfat"),
					packs.Name("mkfs.fat"),
				),
				Severity:    sevCritical,
				Confidence:  confHigh,
				Reason:      "mkfs creates a new filesystem on a device, destroying all existing data on that device",
				Remediation: "Use read-only disk inspection commands instead of formatting commands",
			},
			{
				ID: "rm-rf-system",
				Match: packs.And(
					packs.Name("rm"),
					packs.Or(
						packs.Flags("-rf"),
						packs.Flags("-fr"),
						packs.And(
							packs.Or(packs.Flags("-r"), packs.Flags("-R"), packs.Flags("--recursive")),
							packs.Or(packs.Flags("-f"), packs.Flags("--force")),
						),
					),
					packs.ArgContentRegex(`^(/|~)`),
				),
				Severity:    sevCritical,
				Confidence:  confHigh,
				Reason:      "rm -rf on system-level absolute paths can recursively delete critical operating system or user data",
				Remediation: "Delete specific files or directories individually rather than using recursive force delete",
			},
			{
				ID: "rm-rf-local",
				Match: packs.And(
					packs.Name("rm"),
					packs.Or(
						packs.Flags("-rf"),
						packs.Flags("-fr"),
						packs.And(
							packs.Or(packs.Flags("-r"), packs.Flags("-R"), packs.Flags("--recursive")),
							packs.Or(packs.Flags("-f"), packs.Flags("--force")),
						),
					),
					packs.Not(packs.ArgContentRegex(`^(/|~)`)),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "rm -rf on relative paths recursively deletes files and directories without confirmation",
				Remediation: "Delete specific files or directories individually rather than using recursive force delete",
			},
			{
				ID: "dd-write",
				Match: packs.And(
					packs.Name("dd"),
					packs.ArgPrefix("of="),
				),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "dd writes directly to devices or files, overwriting existing data without confirmation",
				Remediation: "Use cp for file copies instead of raw device writes",
			},
			{
				ID:          "shred-any",
				Match:       packs.Name("shred"),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "shred overwrites files with random data to prevent recovery",
				Remediation: "Move files to archive storage instead of shredding",
			},
			{
				ID: "rm-recursive",
				Match: packs.And(
					packs.Name("rm"),
					packs.Or(packs.Flags("-r"), packs.Flags("-R"), packs.Flags("--recursive")),
					packs.Not(packs.Or(
						packs.Flags("-f"),
						packs.Flags("--force"),
						packs.Flags("-rf"),
						packs.Flags("-fr"),
					)),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "rm -r recursively deletes files and directories",
				Remediation: "Consider deleting specific items rather than entire directory trees",
			},
			{
				ID: "chmod-recursive",
				Match: packs.And(
					packs.Name("chmod"),
					packs.Or(packs.Flags("-R"), packs.Flags("--recursive")),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "chmod -R recursively changes file permissions",
				Remediation: "Apply permission changes to specific files rather than recursively",
			},
			{
				ID: "chmod-777",
				Match: packs.And(
					packs.Name("chmod"),
					packs.RawTextRegex(reChmod777),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "chmod 777 makes files world-writable",
				Remediation: "Use the minimum required permissions instead of world-writable mode",
			},
			{
				ID: "chown-recursive",
				Match: packs.And(
					packs.Name("chown"),
					packs.Or(packs.Flags("-R"), packs.Flags("--recursive")),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "chown -R recursively changes file ownership",
				Remediation: "Apply ownership changes to specific files rather than recursively",
			},
			{
				ID: "mv-to-devnull",
				Match: packs.And(
					packs.Name("mv"),
					packs.Arg("/dev/null"),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "mv to /dev/null can discard files or indicates risky destructive intent",
				Remediation: "Use explicit delete operations on confirmed targets instead of moving to /dev/null",
			},
			{
				ID: "chmod-000",
				Match: packs.And(
					packs.Name("chmod"),
					packs.RawTextRegex(reChmod000),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "chmod 000 removes all permissions, making files inaccessible",
				Remediation: "Set only the minimum restrictive permissions needed rather than removing all access",
			},
			{
				ID: "truncate-zero",
				Match: packs.And(
					packs.Name("truncate"),
					packs.Or(packs.Flags("-s"), packs.Flags("--size")),
					packs.Or(
						packs.RawTextRegex(reTruncate0),
						packs.And(packs.RawTextRegex(reTruncateS0), packs.RawTextContains(" 0 ")),
						packs.RawTextRegex(reTruncateSz0),
					),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "truncate -s 0 empties file contents completely",
				Remediation: "Write output to a new file instead of truncating existing files",
			},
		},
	}
}
