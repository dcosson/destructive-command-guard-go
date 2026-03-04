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
		Destructive: []packs.Rule{
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
				Remediation: "Specify a specific directory instead of /",
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
				Remediation: "Verify the device is correct and not mounted. Back up data first.",
			},
			{
				ID: "rm-recursive-force",
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
				),
				Severity:    sevCritical,
				Confidence:  confHigh,
				Reason:      "rm -rf recursively deletes files and directories without confirmation",
				Remediation: "Use rm -ri (interactive) or ls first to preview what will be deleted",
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
				Remediation: "Double-check the of= parameter before running dd",
			},
			{
				ID:          "shred-any",
				Match:       packs.Name("shred"),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "shred overwrites files with random data to prevent recovery",
				Remediation: "Verify target files before running shred",
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
				Remediation: "Use rm -ri (interactive) to confirm each deletion",
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
				Remediation: "Verify target directory and permission mode before recursive chmod",
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
				Remediation: "Use more restrictive permissions like 644 or 755",
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
				Remediation: "Verify target directory and owner before recursive chown",
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
				Remediation: "Use explicit delete commands and verify target paths",
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
				Remediation: "Use appropriate permissions that preserve required access",
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
				Remediation: "Back up the file first or verify this is intended",
			},
		},
	}
}
