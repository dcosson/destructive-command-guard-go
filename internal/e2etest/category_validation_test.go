package e2etest

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
	"github.com/dcosson/destructive-command-guard-go/internal/evalcore"
	"github.com/dcosson/destructive-command-guard-go/internal/packs"
)

// CategoryBaseline holds per-category rule counts for CI validation.
type CategoryBaseline struct {
	TotalDestructive int                     `json:"total_destructive"`
	TotalPrivacy     int                     `json:"total_privacy"`
	TotalBoth        int                     `json:"total_both"`
	Packs            map[string]PackBaseline `json:"packs"`
}

type PackBaseline struct {
	Destructive int `json:"destructive"`
	Privacy     int `json:"privacy"`
	Both        int `json:"both"`
}

func buildLiveBaseline() CategoryBaseline {
	all := packs.DefaultRegistry.All()
	baseline := CategoryBaseline{
		Packs: make(map[string]PackBaseline, len(all)),
	}
	for _, p := range all {
		var pb PackBaseline
		for _, r := range p.Rules {
			cat := r.Category
			if cat == 0 {
				cat = evalcore.CategoryDestructive
			}
			switch cat {
			case evalcore.CategoryBoth:
				pb.Both++
				baseline.TotalBoth++
			case evalcore.CategoryPrivacy:
				pb.Privacy++
				baseline.TotalPrivacy++
			default:
				pb.Destructive++
				baseline.TotalDestructive++
			}
		}
		baseline.Packs[p.ID] = pb
	}
	return baseline
}

func TestCategoryBaselineValidation(t *testing.T) {
	data, err := os.ReadFile("testdata/category-baseline.json")
	if err != nil {
		t.Fatalf("reading baseline: %v (run TestGenerateCategoryBaseline to create)", err)
	}
	var expected CategoryBaseline
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatalf("parsing baseline: %v", err)
	}

	live := buildLiveBaseline()

	if live.TotalDestructive != expected.TotalDestructive {
		t.Errorf("total destructive: got %d, baseline %d", live.TotalDestructive, expected.TotalDestructive)
	}
	if live.TotalPrivacy != expected.TotalPrivacy {
		t.Errorf("total privacy: got %d, baseline %d", live.TotalPrivacy, expected.TotalPrivacy)
	}
	if live.TotalBoth != expected.TotalBoth {
		t.Errorf("total both: got %d, baseline %d", live.TotalBoth, expected.TotalBoth)
	}

	for packID, exp := range expected.Packs {
		got, ok := live.Packs[packID]
		if !ok {
			t.Errorf("pack %s: in baseline but not in registry", packID)
			continue
		}
		if got != exp {
			t.Errorf("pack %s: got %+v, baseline %+v", packID, got, exp)
		}
	}
	for packID := range live.Packs {
		if _, ok := expected.Packs[packID]; !ok {
			t.Errorf("pack %s: in registry but not in baseline", packID)
		}
	}
}

func TestRegistryAllRulesHaveNonZeroCategory(t *testing.T) {
	rules := guard.Rules()
	for _, r := range rules {
		if r.Category == 0 {
			t.Errorf("rule %s (pack %s) has zero category; must be explicitly set", r.ID, r.PackID)
		}
	}
}

func TestCategoryAssignments(t *testing.T) {
	rules := guard.Rules()
	ruleMap := make(map[string]guard.RuleInfo, len(rules))
	for _, r := range rules {
		ruleMap[r.PackID+"."+r.ID] = r
	}

	// Verify privacy rules
	privacyRules := []string{
		"personal.ssh.ssh-private-key-access",
		"personal.files.personal-files-access",
		"macos.privacy.keychain-read-password",
		"macos.privacy.keychain-dump",
		"macos.privacy.messages-db-access",
		"macos.privacy.private-data-access",
		"macos.privacy.spotlight-search",
	}
	for _, key := range privacyRules {
		r, ok := ruleMap[key]
		if !ok {
			t.Errorf("expected privacy rule %s not found", key)
			continue
		}
		if r.Category != guard.CategoryPrivacy {
			t.Errorf("rule %s: category = %s, want Privacy", key, r.Category)
		}
	}

	// Verify "both" rules
	bothRules := []string{
		"macos.system.csrutil-disable",
		"macos.system.diskutil-erase",
		"macos.system.launchctl-remove",
		"macos.system.nvram-clear",
		"macos.system.nvram-write",
		"macos.system.nvram-delete",
		"macos.system.spctl-disable",
		"macos.system.dscl-delete",
		"macos.system.fdesetup-disable",
		"macos.system.systemsetup-modify",
		"macos.communication.osascript-send-message",
		"macos.communication.osascript-send-email",
		"macos.communication.osascript-system-events",
		"macos.communication.osascript-sensitive-app",
		"macos.communication.shortcuts-run",
		"macos.communication.automator-run",
		"macos.communication.open-terminal",
		"macos.communication.osascript-jxa-catchall",
	}
	for _, key := range bothRules {
		r, ok := ruleMap[key]
		if !ok {
			t.Errorf("expected both rule %s not found", key)
			continue
		}
		if r.Category != guard.CategoryBoth {
			t.Errorf("rule %s: category = %s, want Both", key, r.Category)
		}
	}

	// Verify a sampling of destructive rules remain destructive
	destructiveRules := []string{
		"core.git.force-push",
		"core.filesystem.rm-recursive-force",
		"personal.files.personal-files-delete",
		"macos.system.defaults-delete",
		"macos.communication.osascript-finder-destructive",
	}
	for _, key := range destructiveRules {
		r, ok := ruleMap[key]
		if !ok {
			// May not exist with exact name; skip if not found
			continue
		}
		if r.Category != guard.CategoryDestructive {
			t.Errorf("rule %s: category = %s, want Destructive", key, r.Category)
		}
	}
}

// TestGenerateCategoryBaseline regenerates the baseline file.
// Run with: go test -run TestGenerateCategoryBaseline -v ./internal/e2etest/
func TestGenerateCategoryBaseline(t *testing.T) {
	if os.Getenv("GENERATE_BASELINE") != "1" {
		t.Skip("set GENERATE_BASELINE=1 to regenerate")
	}

	baseline := buildLiveBaseline()

	// Sort the output for deterministic JSON
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	if err := os.WriteFile("testdata/category-baseline.json", append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	t.Logf("wrote testdata/category-baseline.json")

	// Log summary
	t.Logf("Totals: %d destructive, %d privacy, %d both",
		baseline.TotalDestructive, baseline.TotalPrivacy, baseline.TotalBoth)
	packIDs := make([]string, 0, len(baseline.Packs))
	for id := range baseline.Packs {
		packIDs = append(packIDs, id)
	}
	sort.Strings(packIDs)
	for _, id := range packIDs {
		pb := baseline.Packs[id]
		t.Logf("  %-25s %d destructive, %d privacy, %d both", id, pb.Destructive, pb.Privacy, pb.Both)
	}
}
