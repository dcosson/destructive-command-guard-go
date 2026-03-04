package platform

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

func githubPack() packs.Pack {
	asciiOwnerRepo := regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	repoTargetAt := func(idx int) packs.MatchFunc {
		return packs.MatchFunc(func(cmd packs.Command) bool {
			if idx < 0 || idx >= len(cmd.Args) {
				return false
			}
			return asciiOwnerRepo.MatchString(cmd.Args[idx])
		})
	}

	return packs.Pack{
		ID:          "platform.github",
		Name:        "GitHub CLI",
		Description: "GitHub CLI destructive operations",
		Keywords:    []string{"gh", "github"},
		Safe: []packs.Rule{
			{ID: "gh-issue-list-safe", Match: packs.And(packs.Name("gh"), packs.ArgAt(0, "issue"), packs.ArgAt(1, "list"))},
			{ID: "gh-pr-list-safe", Match: packs.And(packs.Name("gh"), packs.ArgAt(0, "pr"), packs.ArgAt(1, "list"))},
		},
		Destructive: []packs.Rule{
			{ID: "gh-repo-delete", Severity: sevCritical, Confidence: confHigh, Reason: "gh repo delete permanently removes a repository", Remediation: "Confirm archival/backups and owner/repo target before deletion", Match: packs.And(packs.Name("gh"), packs.ArgAt(0, "repo"), packs.ArgAt(1, "delete"), repoTargetAt(2))},
			{ID: "gh-release-delete", Severity: sevHigh, Confidence: confHigh, Reason: "gh release delete removes published release artifacts", Remediation: "Verify release/tag and downstream distribution dependencies", Match: packs.And(packs.Name("gh"), packs.ArgAt(0, "release"), packs.ArgAt(1, "delete"))},
			{ID: "gh-issue-pr-close", Severity: sevLow, Confidence: confMedium, Reason: "gh issue/pr close changes workflow state and can disrupt active collaboration", Remediation: "Confirm issue/PR status and team expectations before closing", Match: packs.And(packs.Name("gh"), packs.Or(
				packs.And(packs.ArgAt(0, "issue"), packs.ArgAt(1, "close"), packs.Flags("--yes")),
				packs.And(packs.ArgAt(0, "pr"), packs.ArgAt(1, "close"), packs.Flags("--yes")),
			))},
		},
	}
}
