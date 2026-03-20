package integration

import (
	"runtime"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestPropertyPersonalPackReachability(t *testing.T) {
	cases := []struct {
		pack string
		rule string
		cmd  string
	}{
		{"personal.files", "desktop-delete", "rm ~/Desktop/file.txt"},
		{"personal.files", "downloads-overwrite", "cp file.txt ~/Downloads/"},
		{"personal.files", "documents-modify", "chmod 777 ~/Documents/script.sh"},
		{"personal.files", "desktop-write", "tee ~/Desktop/output.txt"},
		{"personal.files", "documents-access", "cat ~/Documents/notes.txt"},
		{"personal.ssh", "ssh-directory-destructive", "rm -rf ~/.ssh/"},
		{"personal.ssh", "ssh-private-key-access", "cat ~/.ssh/id_rsa"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.pack+"/"+tc.rule, func(t *testing.T) {
			if !HasRegisteredPack(tc.pack) {
				t.Skipf("pack %s not registered", tc.pack)
			}
			res := guard.Evaluate(tc.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
			if !hasRuleMatch(res, tc.pack, tc.rule) {
				t.Fatalf("expected %s/%s for %q; got %+v", tc.pack, tc.rule, tc.cmd, res.Matches)
			}
		})
	}
}

func TestPropertyPersonalSSHSafePaths(t *testing.T) {
	if !HasRegisteredPack("personal.ssh") {
		t.Skip("personal.ssh pack not registered")
	}
	safe := []string{
		"cat ~/.ssh/id_rsa.pub",
		"ssh-copy-id -i ~/.ssh/id_ed25519.pub user@host",
		"cat ~/.ssh/config",
		"grep Host ~/.ssh/config",
	}
	for _, cmd := range safe {
		res := guard.Evaluate(cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		if res.Decision != guard.Allow {
			t.Fatalf("expected allow for safe ssh command %q, got %s", cmd, res.Decision)
		}
	}
}

func TestPropertyPersonalFilesSeverityTiers(t *testing.T) {
	if !HasRegisteredPack("personal.files") {
		t.Skip("personal.files pack not registered")
	}
	tiers := []struct {
		cmd      string
		severity guard.Severity
		rule     string
	}{
		{"rm ~/Desktop/file.txt", guard.High, "desktop-delete"},
		{"cp file.txt ~/Downloads/", guard.High, "downloads-overwrite"},
		{"cat ~/Documents/notes.txt", guard.Medium, "documents-access"},
	}
	for _, tc := range tiers {
		res := guard.Evaluate(tc.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		if !hasRuleMatch(res, "personal.files", tc.rule) {
			t.Fatalf("expected rule %s for %q", tc.rule, tc.cmd)
		}
		gotSeverity := guard.Indeterminate
		switch {
		case res.DestructiveAssessment != nil:
			gotSeverity = res.DestructiveAssessment.Severity
		case res.PrivacyAssessment != nil:
			gotSeverity = res.PrivacyAssessment.Severity
		}
		if gotSeverity != tc.severity {
			t.Fatalf("severity mismatch for %q: got=%v want=%v", tc.cmd, gotSeverity, tc.severity)
		}
	}
}

func TestPropertyMacOSPackRegistrationGated(t *testing.T) {
	ids := []string{"macos.communication", "macos.privacy", "macos.system"}
	for _, id := range ids {
		if !HasRegisteredPack(id) {
			t.Fatalf("expected %s to be registered on all platforms", id)
		}
	}
}

func TestPropertyMacOSDeterministicRules(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS pack tests only on darwin")
	}
	cases := []struct {
		pack string
		rule string
		cmd  string
	}{
		{"macos.communication", "osascript-send-message", `osascript -e 'tell application "Messages" to send "hello" to buddy "John"'`},
		{"macos.communication", "shortcuts-run", `shortcuts run "My Shortcut"`},
		{"macos.privacy", "keychain-read-password", "security find-generic-password -s MyService"},
		{"macos.privacy", "spotlight-search", `mdfind "tax return"`},
		{"macos.system", "csrutil-disable", "csrutil disable"},
		{"macos.system", "defaults-write", "defaults write com.apple.dock autohide -bool true"},
	}
	for _, tc := range cases {
		res := guard.Evaluate(tc.cmd, guard.WithDestructivePolicy(guard.InteractivePolicy()))
		if !hasRuleMatch(res, tc.pack, tc.rule) {
			t.Fatalf("expected %s/%s for %q", tc.pack, tc.rule, tc.cmd)
		}
	}
}
