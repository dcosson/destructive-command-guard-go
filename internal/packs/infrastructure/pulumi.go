package infrastructure

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func pulumiPack() packs.Pack {
	yesFlag := packs.Or(packs.Flags("--yes"), packs.Flags("-y"))
	return packs.Pack{
		ID:          "infrastructure.pulumi",
		Name:        "Pulumi",
		Description: "Pulumi infrastructure destructive operations",
		Keywords:    []string{"pulumi"},
		Safe: []packs.Rule{
			{ID: "pulumi-preview-safe", Match: packs.And(packs.Name("pulumi"), packs.ArgAt(0, "preview"))},
			{ID: "pulumi-stack-safe", Match: packs.And(packs.Name("pulumi"), packs.ArgAt(0, "stack"), packs.ArgAt(1, "ls"))},
		},
		Destructive: []packs.Rule{
			{ID: "pulumi-destroy-yes", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "pulumi destroy with yes bypasses confirmation", Remediation: "Remove yes flag and verify target stack", Match: packs.And(packs.Name("pulumi"), packs.ArgAt(0, "destroy"), yesFlag)},
			{ID: "pulumi-destroy", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "pulumi destroy removes provisioned resources", Remediation: "Confirm stack and environment before destroy", Match: packs.And(packs.Name("pulumi"), packs.ArgAt(0, "destroy"), packs.Not(yesFlag))},
			{ID: "pulumi-up-yes", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "pulumi up with yes applies changes without confirmation", Remediation: "Review preview and require manual confirmation", Match: packs.And(packs.Name("pulumi"), packs.ArgAt(0, "up"), yesFlag)},
			{ID: "pulumi-up", Severity: sevMedium, Confidence: confMedium, EnvSensitive: true, Reason: "pulumi up mutates infrastructure resources", Remediation: "Run preview and inspect changes", Match: packs.And(packs.Name("pulumi"), packs.ArgAt(0, "up"), packs.Not(yesFlag))},
		},
	}
}
