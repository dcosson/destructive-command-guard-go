package macos

import (
	"regexp"
	"strings"

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

var (
	osascriptMessagesRe      = regexp.MustCompile(`(?i)tell\s+application\s+"Messages"`)
	osascriptMailRe          = regexp.MustCompile(`(?i)tell\s+application\s+"Mail"`)
	osascriptSystemEventsRe  = regexp.MustCompile(`(?i)tell\s+application\s+"System Events"`)
	osascriptSensitiveAppsRe = regexp.MustCompile(`(?i)tell\s+application\s+"(Contacts|Calendar|Reminders|Notes|Safari)"`)
	osascriptDisplayRe       = regexp.MustCompile(`(?i)(?:display\s+(?:dialog|notification|alert)|say\s+)`)
	osascriptFinderBenignRe  = regexp.MustCompile(`(?i)tell\s+application\s+"Finder"\s+to\s+(?:get|open\s+folder|reveal)`)
	osascriptFinderDestRe    = regexp.MustCompile(`(?i)tell\s+application\s+"Finder"\s+to\s+(?:delete|empty\s+trash|move\s+.+\s+to\s+trash)`)
)

func communicationPack() packs.Pack {
	openTerminal := packs.MatchFunc(func(cmd packs.Command) bool {
		if !strings.EqualFold(cmd.Name, "open") {
			return false
		}
		if _, ok := cmd.Flags["-a"]; !ok {
			return false
		}
		for _, arg := range cmd.Args {
			target := strings.ToLower(arg)
			if strings.Contains(target, "terminal") || strings.Contains(target, "iterm") {
				return true
			}
		}
		for _, arg := range cmd.RawArgs {
			target := strings.ToLower(arg)
			if strings.Contains(target, "terminal") || strings.Contains(target, "iterm") {
				return true
			}
		}
		return false
	})

	jxaCatchAll := packs.And(
		packs.Name("osascript"),
		packs.ArgContains("javascript"),
	)

	return packs.Pack{
		ID:          "macos.communication",
		Name:        "macOS Communication",
		Description: "Detects osascript/Shortcuts commands that send messages, emails, or control apps",
		Keywords:    []string{"osascript", "shortcuts", "automator", "Terminal", "iTerm"},
		Safe: []packs.Rule{
			{ID: "osascript-display", Match: packs.And(
				packs.Name("osascript"),
				packs.ArgContentRegex(osascriptDisplayRe.String()),
				packs.Not(packs.ArgContentRegex(osascriptMessagesRe.String())),
				packs.Not(packs.ArgContentRegex(osascriptMailRe.String())),
				packs.Not(packs.ArgContentRegex(osascriptSystemEventsRe.String())),
				packs.Not(packs.ArgContentRegex(osascriptSensitiveAppsRe.String())),
			)},
			{ID: "osascript-finder-benign", Match: packs.And(
				packs.Name("osascript"),
				packs.ArgContentRegex(osascriptFinderBenignRe.String()),
				packs.Not(packs.ArgContentRegex(osascriptMessagesRe.String())),
				packs.Not(packs.ArgContentRegex(osascriptMailRe.String())),
				packs.Not(packs.ArgContentRegex(osascriptSystemEventsRe.String())),
				packs.Not(packs.ArgContentRegex(osascriptSensitiveAppsRe.String())),
			)},
		},
		Rules: []packs.Rule{
			{ID: "osascript-send-message", Severity: sevCritical, Confidence: confHigh, Reason: "osascript can send iMessages automatically", Remediation: "Use read-only AppleScript commands without send operations", Match: packs.And(packs.Name("osascript"), packs.ArgContentRegex(osascriptMessagesRe.String()))},
			{ID: "osascript-send-email", Severity: sevCritical, Confidence: confHigh, Reason: "osascript can send email automatically through Mail.app", Remediation: "Use read-only AppleScript commands without send operations", Match: packs.And(packs.Name("osascript"), packs.ArgContentRegex(osascriptMailRe.String()))},
			{ID: "osascript-system-events", Severity: sevCritical, Confidence: confHigh, Reason: "System Events can automate arbitrary GUI actions", Remediation: "Do not allow System Events automation", Match: packs.And(packs.Name("osascript"), packs.ArgContentRegex(osascriptSystemEventsRe.String()))},
			{ID: "osascript-sensitive-app", Severity: sevHigh, Confidence: confHigh, Reason: "osascript targets sensitive macOS applications", Remediation: "Use native CLI tooling instead of app automation scripts", Match: packs.And(packs.Name("osascript"), packs.ArgContentRegex(osascriptSensitiveAppsRe.String()))},
			{ID: "shortcuts-run", Severity: sevHigh, Confidence: confHigh, Reason: "shortcuts run executes automation actions with side effects", Remediation: "Use direct CLI commands for required operations", Match: packs.And(packs.Name("shortcuts"), packs.ArgAt(0, "run"))},
			{ID: "automator-run", Severity: sevHigh, Confidence: confMedium, Reason: "automator executes workflows with arbitrary side effects", Remediation: "Use direct CLI commands for required operations", Match: packs.And(packs.Name("automator"), packs.Not(packs.Or(packs.Flags("--help"), packs.Flags("-h"), packs.Flags("--version"))))},
			{ID: "osascript-finder-destructive", Severity: sevHigh, Confidence: confHigh, Reason: "osascript performs destructive Finder operations", Remediation: "Use non-destructive file queries instead of Finder delete operations", Match: packs.And(packs.Name("osascript"), packs.ArgContentRegex(osascriptFinderDestRe.String()))},
			{ID: "open-terminal", Severity: sevHigh, Confidence: confMedium, Reason: "Opening Terminal/iTerm may bypass guard supervision", Remediation: "Do not spawn external terminals from automated flows", Match: openTerminal},
			{ID: "osascript-jxa-catchall", Severity: sevMedium, Confidence: confLow, Reason: "osascript JavaScript mode can automate applications with side effects", Remediation: "Use explicit shell commands instead of JXA automation", Match: jxaCatchAll},
		},
	}
}
