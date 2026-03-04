package macos

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func systemPack() packs.Pack {
	return packs.Pack{
		ID:          "macos.system",
		Name:        "macOS System",
		Description: "Detects macOS system modification commands (preferences, services, disks, security features)",
		Keywords: []string{
			"defaults", "launchctl", "diskutil", "csrutil", "tmutil", "nvram", "spctl", "systemsetup", "dscl", "fdesetup", "bless",
		},
		Safe: []packs.Rule{
			{ID: "defaults-read", Match: packs.And(packs.Name("defaults"), packs.ArgAt(0, "read"))},
			{ID: "launchctl-list", Match: packs.And(packs.Name("launchctl"), packs.Or(packs.ArgAt(0, "list"), packs.ArgAt(0, "print"), packs.ArgAt(0, "blame")))},
			{ID: "nvram-read", Match: packs.And(packs.Name("nvram"), packs.Or(packs.Flags("-p"), packs.Flags("-x"), packs.Flags("--print")))},
			{ID: "diskutil-info", Match: packs.And(packs.Name("diskutil"), packs.Or(packs.ArgAt(0, "info"), packs.ArgAt(0, "list")))},
			{ID: "diskutil-apfs-list", Match: packs.And(packs.Name("diskutil"), packs.ArgAt(0, "apfs"), packs.ArgAt(1, "list"))},
		},
		Destructive: []packs.Rule{
			{ID: "csrutil-disable", Severity: sevCritical, Confidence: confHigh, Reason: "Disabling SIP weakens core OS protections", Remediation: "Do not disable SIP", Match: packs.And(packs.Name("csrutil"), packs.ArgAt(0, "disable"))},
			{ID: "diskutil-erase", Severity: sevCritical, Confidence: confHigh, Reason: "Disk erase/repartition causes irreversible data loss", Remediation: "Do not erase or repartition disks", Match: packs.And(
				packs.Name("diskutil"),
				packs.Or(
					packs.ArgAt(0, "eraseDisk"),
					packs.ArgAt(0, "eraseVolume"),
					packs.ArgAt(0, "partitionDisk"),
					packs.ArgAt(0, "secureErase"),
					packs.And(packs.ArgAt(0, "apfs"), packs.Or(
						packs.ArgAt(1, "deleteContainer"),
						packs.ArgAt(1, "deleteVolume"),
						packs.ArgAt(1, "resizeContainer"),
					)),
				),
			)},
			{ID: "launchctl-remove", Severity: sevCritical, Confidence: confHigh, Reason: "Removing/disabling/killing launch services can break system behavior", Remediation: "Do not modify launch services", Match: packs.And(
				packs.Name("launchctl"),
				packs.Or(
					packs.ArgAt(0, "remove"),
					packs.ArgAt(0, "unload"),
					packs.ArgAt(0, "bootout"),
					packs.ArgAt(0, "disable"),
					packs.ArgAt(0, "kickstart"),
					packs.ArgAt(0, "kill"),
				),
			)},
			{ID: "tmutil-delete", Severity: sevCritical, Confidence: confHigh, Reason: "Deleting Time Machine backups removes recovery points", Remediation: "Do not delete backups", Match: packs.And(
				packs.Name("tmutil"),
				packs.Or(packs.ArgAt(0, "delete"), packs.ArgAt(0, "deletelocalsnapshots")),
			)},
			{ID: "nvram-clear", Severity: sevCritical, Confidence: confHigh, Reason: "Clearing all NVRAM variables can affect boot behavior", Remediation: "Do not clear NVRAM", Match: packs.And(packs.Name("nvram"), packs.Flags("-c"))},
			{ID: "nvram-write", Severity: sevCritical, Confidence: confHigh, Reason: "Modifying NVRAM variables can affect boot behavior", Remediation: "Do not write NVRAM variables", Match: packs.And(
				packs.Name("nvram"),
				packs.ArgContentRegex(`=`),
				packs.Not(packs.Or(packs.Flags("-p"), packs.Flags("-x"), packs.Flags("--print"))),
			)},
			{ID: "nvram-delete", Severity: sevCritical, Confidence: confHigh, Reason: "Deleting NVRAM variables can affect boot behavior", Remediation: "Do not delete NVRAM variables", Match: packs.And(packs.Name("nvram"), packs.Flags("-d"))},
			{ID: "spctl-disable", Severity: sevCritical, Confidence: confHigh, Reason: "Disabling Gatekeeper allows unsigned apps", Remediation: "Do not disable Gatekeeper", Match: packs.And(
				packs.Name("spctl"),
				packs.Or(packs.Flags("--master-disable"), packs.Flags("--disable")),
			)},
			{ID: "dscl-delete", Severity: sevCritical, Confidence: confHigh, Reason: "Modifying directory services affects user/group management", Remediation: "Do not modify macOS directory services", Match: packs.And(
				packs.Name("dscl"),
				packs.Or(
					packs.ArgContains("-delete"),
					packs.ArgContains("delete"),
					packs.ArgContains("-create"),
					packs.ArgContains("create"),
				),
			)},
			{ID: "fdesetup-disable", Severity: sevCritical, Confidence: confHigh, Reason: "Disabling/modifying FileVault weakens disk security", Remediation: "Do not modify FileVault settings", Match: packs.And(
				packs.Name("fdesetup"),
				packs.Or(packs.ArgAt(0, "disable"), packs.ArgAt(0, "removeuser"), packs.ArgAt(0, "destroy")),
			)},
			{ID: "defaults-delete", Severity: sevHigh, Confidence: confHigh, Reason: "defaults delete removes preference keys and can break app or system behavior", Remediation: "Use defaults read for non-mutating preference inspection", Match: packs.And(packs.Name("defaults"), packs.ArgAt(0, "delete"))},
			{ID: "defaults-write", Severity: sevHigh, Confidence: confMedium, Reason: "defaults write changes app or system preference behavior", Remediation: "Use defaults read for non-mutating preference inspection", Match: packs.And(packs.Name("defaults"), packs.ArgAt(0, "write"))},
			{ID: "systemsetup-modify", Severity: sevHigh, Confidence: confMedium, Reason: "systemsetup modifies machine-level configuration", Remediation: "Use getter flags for read-only inspection", Match: packs.And(
				packs.Name("systemsetup"),
				packs.Not(packs.Or(
					packs.Flags("-getcomputername"),
					packs.Flags("-getlocalsubnetname"),
					packs.Flags("-getstartupdisk"),
					packs.Flags("-getremotelogin"),
					packs.Flags("-getremoteappleevents"),
					packs.Flags("-gettimezone"),
				)),
			)},
		},
	}
}
