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
						packs.Flags("--force-if-includes"),
						packs.Flags("--mirror"),
						packs.Flags("--delete"),
						packs.Flags("-d"),
					)),
					packs.Not(packs.ArgContentRegex(`^:`)),
					packs.Not(packs.ArgContentRegex(`^\+`)),
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
		Rules: []packs.Rule{
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
				Remediation: "Push without force or merge before pushing",
			},
			{
				ID: "git-push-force-with-lease",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.Flags("--force-with-lease"),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git push --force-with-lease overwrites remote history with a safety check, but still force-pushes",
				Remediation: "Push without force or merge before pushing",
			},
			{
				ID: "git-push-force-if-includes",
				Match: packs.And(
					packs.Name("git"),
					packs.ArgAt(0, "push"),
					packs.Flags("--force-if-includes"),
				),
				Severity:    sevMedium,
				Confidence:  confHigh,
				Reason:      "git push --force-if-includes can overwrite remote history when divergence checks pass",
				Remediation: "Push without force or merge before pushing",
			},
			{
				ID:          "git-push-mirror",
				Match:       packs.And(packs.Name("git"), packs.ArgAt(0, "push"), packs.Flags("--mirror")),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git push --mirror overwrites all remote refs to match local, deleting remote branches not present locally",
				Remediation: "Push explicit refs instead of mirroring all refs",
			},
			{
				ID:          "git-reset-hard",
				Match:       packs.And(packs.Name("git"), packs.ArgAt(0, "reset"), packs.Flags("--hard")),
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "git reset --hard discards all uncommitted changes permanently",
				Remediation: "Use git reset --soft or git reset --mixed",
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
				Remediation: "Restore only required files with git restore <path>",
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
				Remediation: "Use git merge to preserve branch history",
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
				Remediation: "Remove specific paths explicitly with rm <path>",
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
				Remediation: "Remove specific paths explicitly with rm <path>",
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
				Remediation: "Use git branch -d to delete only merged branches",
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
				Remediation: "Keep the ref and deprecate it through branch policy",
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
				Remediation: "Keep stashes or apply them with git stash apply",
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
				Remediation: "Restore only required files with git restore <path>",
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
				Remediation: "Restore only required files with git restore <path>",
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
				Remediation: "Keep the ref and deprecate it through branch policy",
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
				Remediation: "Push without force or merge before pushing",
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
				Remediation: "Restore only required files with git restore <path>",
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
				Remediation: "Read historical content with git show <commit>:<path>",
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
				Remediation: "Keep reflog entries and rely on default expiration",
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
				Remediation: "Run git gc without explicit prune flags",
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
				Remediation: "Use targeted commit edits instead of full history rewrite",
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
				Remediation: "Restore only required files with git restore <path>",
			},
		},
	}
}
