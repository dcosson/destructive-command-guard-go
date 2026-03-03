package guard_test

import (
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

func TestFaultEvaluateNilAndInvalidOptions(t *testing.T) {
	_ = guard.Evaluate("git push --force", guard.WithPolicy(nil))
	_ = guard.Evaluate("ls", nil)
	_ = guard.Evaluate("ls", guard.WithAllowlist(), guard.WithBlocklist())
	res := guard.Evaluate("ls", guard.WithPacks("nonexistent.pack"))
	found := false
	for _, w := range res.Warnings {
		if w.Code == guard.WarnUnknownPackID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected WarnUnknownPackID warning for unknown pack")
	}
}

func TestSecurityBlocklistSeparatorBypass(t *testing.T) {
	cases := []struct {
		name        string
		cmd         string
		block       string
		shouldBlock bool
	}{
		{name: "direct-rm", cmd: "rm -rf /", block: "rm *", shouldBlock: true},
		{name: "simple-rm", cmd: "rm file.txt", block: "rm *", shouldBlock: true},
		{name: "separator-bypass", cmd: "echo safe; rm -rf /", block: "rm *", shouldBlock: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res := guard.Evaluate(tc.cmd, guard.WithBlocklist(tc.block))
			if tc.shouldBlock && res.Decision != guard.Deny {
				t.Fatalf("expected deny for %q, got %s", tc.cmd, res.Decision)
			}
			if !tc.shouldBlock && res.Decision == guard.Deny && len(res.Matches) == 1 && res.Matches[0].Pack == "_blocklist" {
				t.Fatalf("unexpected blocklist deny for separator command %q", tc.cmd)
			}
		})
	}
}
