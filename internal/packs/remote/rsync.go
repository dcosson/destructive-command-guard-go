package remote

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

const (
	sevLow      = 1
	sevMedium   = 2
	sevHigh     = 3
	sevCritical = 4

	confLow    = 0
	confMedium = 1
	confHigh   = 2
)

func rsyncPack() packs.Pack {
	return packs.Pack{
		ID:          "remote.rsync",
		Name:        "rsync",
		Description: "rsync destructive synchronization operations",
		Keywords:    []string{"rsync"},
		Safe: []packs.Rule{
			{ID: "rsync-copy-safe", Match: packs.And(packs.Name("rsync"), packs.Not(packs.RawTextContains("--delete")), packs.Not(packs.Flags("--remove-source-files")))},
		},
		Rules: []packs.Rule{
			{ID: "rsync-delete", Severity: sevHigh, Confidence: confHigh, Reason: "rsync --delete removes destination files that are absent from source", Remediation: "Use rsync without --delete to prevent destination removal", Match: packs.And(packs.Name("rsync"), packs.Or(packs.RawTextContains("--delete"), packs.Flags("--remove-source-files")))},
		},
	}
}
