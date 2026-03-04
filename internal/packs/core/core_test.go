package core

import (
	"context"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/internal/packs"
	"github.com/dcosson/destructive-command-guard-go/internal/parse"
)

func TestGitPackInventoryAndReachability(t *testing.T) {
	pk := GitPack()
	if got := len(pk.Safe); got != 10 {
		t.Fatalf("safe pattern count = %d, want 10", got)
	}
	if got := len(pk.Destructive); got != 22 {
		t.Fatalf("destructive pattern count = %d, want 22", got)
	}

	hits := map[string]string{
		"git-push-force":             "git push --force origin main",
		"git-push-force-with-lease":  "git push --force-with-lease origin main",
		"git-push-force-if-includes": "git push --force-if-includes origin main",
		"git-push-mirror":            "git push --mirror",
		"git-reset-hard":             "git reset --hard HEAD~1",
		"git-checkout-discard-all":   "git checkout -- .",
		"git-rebase":                 "git rebase main",
		"git-clean-force":            "git clean -f",
		"git-clean-force-dirs":       "git clean -fd",
		"git-branch-force-delete":    "git branch -D dead-branch",
		"git-push-delete":            "git push origin --delete old-branch",
		"git-stash-drop":             "git stash drop stash@{0}",
		"git-checkout-discard-file":  "git checkout -- app/main.go",
		"git-checkout-dot":           "git checkout .",
		"git-push-refspec-delete":    "git push origin :old-branch",
		"git-push-force-refspec":     "git push origin +main:main",
		"git-restore-worktree-all":   "git restore .",
		"git-restore-source":         "git restore --source HEAD~2 app/main.go",
		"git-reflog-expire":          "git reflog expire --expire=now --all",
		"git-gc-prune":               "git gc --prune=now",
		"git-filter-branch":          "git filter-branch --tree-filter 'rm secrets.txt' HEAD",
		"git-restore-file":           "git restore app/main.go",
	}

	for _, rule := range pk.Destructive {
		cmd, ok := hits[rule.ID]
		if !ok {
			t.Fatalf("missing hit probe for destructive rule %q", rule.ID)
		}
		if !ruleMatches(t, rule, cmd) {
			t.Fatalf("expected rule %q to match %q", rule.ID, cmd)
		}
	}

	safeHits := map[string]string{
		"git-push-safe":           "git push origin main",
		"git-branch-safe":         "git branch -d merged-branch",
		"git-reset-safe":          "git reset --soft HEAD~1",
		"git-rebase-recovery":     "git rebase --abort",
		"git-status":              "git status",
		"git-log":                 "git log --oneline -n 5",
		"git-diff":                "git diff HEAD~1",
		"git-restore-staged-safe": "git restore --staged app/main.go",
		"git-fetch":               "git fetch origin",
		"git-switch-safe":         "git switch main",
	}

	for _, rule := range pk.Safe {
		cmd, ok := safeHits[rule.ID]
		if !ok {
			t.Fatalf("missing hit probe for safe rule %q", rule.ID)
		}
		if !ruleMatches(t, rule, cmd) {
			t.Fatalf("expected safe rule %q to match %q", rule.ID, cmd)
		}
	}
}

func TestFilesystemPackInventoryAndReachability(t *testing.T) {
	pk := FilesystemPack()
	if got := len(pk.Safe); got != 4 {
		t.Fatalf("safe pattern count = %d, want 4", got)
	}
	if got := len(pk.Destructive); got != 12 {
		t.Fatalf("destructive pattern count = %d, want 12", got)
	}

	hits := map[string]string{
		"rm-rf-root":         "rm -rf /",
		"mkfs-any":           "mkfs.ext4 /dev/sda1",
		"rm-recursive-force": "rm -rf /tmp/build",
		"dd-write":           "dd if=/dev/zero of=/dev/sda bs=4M",
		"shred-any":          "shred -vfz file.txt",
		"rm-recursive":       "rm -r /tmp/build",
		"chmod-recursive":    "chmod -R 755 ./app",
		"chmod-777":          "chmod 777 file.txt",
		"chown-recursive":    "chown -R root:root /var/app",
		"mv-to-devnull":      "mv data.db /dev/null",
		"chmod-000":          "chmod 000 secret.txt",
		"truncate-zero":      "truncate -s 0 app.log",
	}

	for _, rule := range pk.Destructive {
		cmd, ok := hits[rule.ID]
		if !ok {
			t.Fatalf("missing hit probe for destructive rule %q", rule.ID)
		}
		if !ruleMatches(t, rule, cmd) {
			t.Fatalf("expected rule %q to match %q", rule.ID, cmd)
		}
	}

	safeHits := map[string]string{
		"rm-single-safe":    "rm file.txt",
		"chmod-single-safe": "chmod 644 file.txt",
		"chown-single-safe": "chown user:group file.txt",
		"mv-safe":           "mv file.txt backup/",
	}

	for _, rule := range pk.Safe {
		cmd, ok := safeHits[rule.ID]
		if !ok {
			t.Fatalf("missing hit probe for safe rule %q", rule.ID)
		}
		if !ruleMatches(t, rule, cmd) {
			t.Fatalf("expected safe rule %q to match %q", rule.ID, cmd)
		}
	}
}

func ruleMatches(t *testing.T, rule packs.Rule, command string) bool {
	t.Helper()
	parser := parse.NewBashParser()
	parsed := parser.ParseAndExtract(context.Background(), command, 0)
	for _, extracted := range parsed.Commands {
		pc := packs.Command{
			Name:    extracted.Name,
			Args:    append([]string{}, extracted.Args...),
			RawArgs: append([]string{}, extracted.RawArgs...),
			Flags:   extracted.Flags,
			RawText: extracted.RawText,
		}
		if rule.Match != nil && rule.Match.Match(pc) {
			return true
		}
	}
	return false
}
