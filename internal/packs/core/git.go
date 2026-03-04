package core

import (
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

func init() {
	packs.DefaultRegistry.Register(GitPack())
}

func GitPack() packs.Pack {
	return packs.Pack{
		ID:          "core.git",
		Name:        "Git",
		Description: "Git version control destructive operations",
		Keywords:    []string{"git"},
		Safe: []packs.Rule{
			{
				ID: "git-push-safe",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.Not(packs.Or(
						packs.Flags("--force"),
						packs.Flags("-f"),
						packs.Flags("--force-with-lease"),
						packs.Flags("--mirror"),
						packs.Flags("--delete"),
						packs.Flags("-d"),
					)),
					packs.Not(packs.ArgContentRegex(`^:`)),
					packs.Not(packs.ArgContentRegex(`^\+`)),
				),
			},
			{
				ID: "git-push-force-with-lease",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.Flags("--force-with-lease"),
					packs.Not(packs.Or(
						packs.Flags("--force"),
						packs.Flags("-f"),
					)),
				),
			},
			{
				ID: "git-branch-safe",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "branch"),
					packs.Not(packs.Or(
						packs.Flags("-D"),
						packs.Flags("--force"),
						packs.Flags("-f"),
					)),
				),
			},
			{
				ID: "git-reset-safe",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "reset"),
					packs.Not(packs.Flags("--hard")),
				),
			},
			{
				ID: "git-rebase-recovery",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "rebase"),
					packs.Or(
						packs.Flags("--abort"),
						packs.Flags("--continue"),
						packs.Flags("--skip"),
					),
				),
			},
			{ID: "git-status", Match: packs.And(packs.Name("git"), packs.ArgAt(0, "status"))},
			{ID: "git-log", Match: packs.And(packs.Name("git"), packs.ArgAt(0, "log"))},
			{ID: "git-diff", Match: packs.And(packs.Name("git"), packs.ArgAt(0, "diff"))},
			{
				ID: "git-restore-staged-safe",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "restore"),
					packs.Flags("--staged"),
					packs.Not(packs.Flags("--worktree")),
				),
			},
			{ID: "git-fetch", Match: packs.And(packs.Name("git"), packs.ArgAt(0, "fetch"))},
			{
				ID: "git-switch-safe",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "switch"),
				),
			},
		},
		Destructive: []packs.Rule{
			{
				ID: "git-push-force",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.Or(packs.Flags("--force"), packs.Flags("-f")),
				),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git push --force overwrites remote history, potentially losing other contributors' commits",
				Remediation: "Use git push --force-with-lease for safer force pushing",
			},
			{
				ID:          "git-push-mirror",
				Match:       packs.And(packs.Name("git"), packs.ArgAt(0, "push"), packs.Flags("--mirror")),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git push --mirror overwrites all remote refs to match local, deleting remote branches not present locally",
				Remediation: "Use explicit branch pushes instead of --mirror",
			},
			{
				ID:          "git-reset-hard",
				Match:       packs.And(packs.Name("git"), packs.ArgAt(0, "reset"), packs.Flags("--hard")),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git reset --hard discards all uncommitted changes permanently",
				Remediation: "Use git stash before git reset --hard, or use git reset --soft/--mixed",
			},
			{
				ID: "git-checkout-discard-all",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "checkout"),
					packs.Flags("--"),
					packs.Arg("."),
				),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git checkout -- . discards all working directory changes permanently",
				Remediation: "Use git stash to save changes, or use git checkout -- <specific-file>",
			},
			{
				ID: "git-rebase",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "rebase"),
					packs.Not(packs.Or(
						packs.Flags("--abort"),
						packs.Flags("--continue"),
						packs.Flags("--skip"),
					)),
				),
				Severity:    sevHigh,
				Confidence:  confMedium,
				Reason:      "git rebase rewrites commit history; rebasing shared branches can lose work for other contributors",
				Remediation: "Only rebase local, unpublished branches. Use git merge for shared branches.",
			},
			{
				ID: "git-clean-force",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "clean"),
					packs.Or(packs.Flags("-f"), packs.Flags("--force")),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git clean -f permanently deletes untracked files",
				Remediation: "Use git clean -n (dry run) first to preview what will be deleted",
			},
			{
				ID: "git-clean-force-dirs",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "clean"),
					packs.Or(packs.Flags("-f"), packs.Flags("--force")),
					packs.Or(packs.Flags("-d"), packs.Flags("-fd"), packs.Flags("-df")),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git clean -fd permanently deletes untracked files and directories",
				Remediation: "Use git clean -nd (dry run with directories) first to preview what will be deleted",
			},
			{
				ID: "git-branch-force-delete",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "branch"),
					packs.Or(
						packs.Flags("-D"),
						packs.And(
							packs.Or(packs.Flags("-d"), packs.Flags("--delete")),
							packs.Or(packs.Flags("-f"), packs.Flags("--force")),
						),
					),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git branch -D force-deletes a local branch even if it has unmerged changes",
				Remediation: "Use git branch -d (lowercase) which refuses to delete unmerged branches",
			},
			{
				ID: "git-push-delete",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.Or(packs.Flags("--delete"), packs.Flags("-d")),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git push --delete removes a branch or tag from the remote",
				Remediation: "Verify the branch/tag name before deleting from remote",
			},
			{
				ID: "git-stash-drop",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "stash"),
					packs.Or(packs.ArgAt(1, "drop"), packs.ArgAt(1, "clear")),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git stash drop/clear permanently discards stashed changes",
				Remediation: "Use git stash list to review stashes before dropping",
			},
			{
				ID: "git-checkout-discard-file",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "checkout"),
					packs.Flags("--"),
					packs.Not(packs.Arg(".")),
				),
				Severity:    sevLow,
				Confidence:  confMedium,
				Reason:      "git checkout -- <file> discards uncommitted changes to specified files",
				Remediation: "Use git stash to save changes first, or verify you want to discard these specific files",
			},
			{
				ID: "git-checkout-dot",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "checkout"),
					packs.Arg("."),
					packs.Not(packs.Flags("--")),
				),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git checkout . discards all working directory changes permanently",
				Remediation: "Use git stash to save changes, or use git checkout <specific-file>",
			},
			{
				ID: "git-push-refspec-delete",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.ArgContentRegex(`^:`),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git push :branch deletes a remote branch using refspec deletion syntax",
				Remediation: "Use git push --delete <branch> for clarity, and verify the branch name",
			},
			{
				ID: "git-push-force-refspec",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.ArgContentRegex(`^\+`),
				),
				Severity:    sevHigh,
				Confidence:  confMedium,
				Reason:      "git push +refspec force-pushes an individual ref, overwriting remote history for that ref",
				Remediation: "Use git push --force-with-lease for safer force pushing",
			},
			{
				ID: "git-restore-worktree-all",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "restore"),
					packs.Arg("."),
				),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git restore . discards all working tree changes permanently",
				Remediation: "Use git stash to save changes first, or restore specific files",
			},
			{
				ID: "git-restore-source",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "restore"),
					packs.Flags("--source"),
				),
				Severity:    sevHigh,
				Confidence:  confMedium,
				Reason:      "git restore --source overwrites working tree files from a specific commit",
				Remediation: "Use git stash first, or verify the source commit is correct",
			},
			{
				ID: "git-reflog-expire",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "reflog"),
					packs.ArgAt(1, "expire"),
				),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git reflog expire removes reflog entries, eliminating the safety net for recovering from git reset --hard and git rebase",
				Remediation: "Avoid expiring reflogs unless you are certain previous states are no longer needed",
			},
			{
				ID: "git-gc-prune",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "gc"),
					packs.Flags("--prune"),
				),
				Severity:    sevMedium,
				Confidence:  confMedium,
				Reason:      "git gc --prune permanently removes unreachable objects, making recovery from reflog impossible",
				Remediation: "Use git gc without --prune (default prune age is safer)",
			},
			{
				ID: "git-filter-branch",
				Match: packs.And(
					packs.Name("git"),
					packs.Or(
						packs.ArgAt(0, "filter-branch"),
						packs.ArgAt(0, "filter-repo"),
					),
				),
				Severity:    sevHigh,
				Confidence:  confMedium,
				Reason:      "git filter-branch/filter-repo rewrites entire repository history, modifying all commits",
				Remediation: "Create a backup branch before running, and verify repository rewrite scope",
			},
			{
				ID: "git-restore-file",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "restore"),
					packs.Not(packs.Arg(".")),
					packs.Not(packs.Flags("--staged")),
					packs.Not(packs.Flags("--source")),
				),
				Severity:    sevLow,
				Confidence:  confMedium,
				Reason:      "git restore <file> discards uncommitted changes to specified files",
				Remediation: "Use git stash to save changes first",
			},
		},
	}
}
