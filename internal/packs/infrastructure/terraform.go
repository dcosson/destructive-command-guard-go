package infrastructure

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

func terraformPack() packs.Pack {
	return packs.Pack{
		ID:          "infrastructure.terraform",
		Name:        "Terraform",
		Description: "Terraform infrastructure destructive operations",
		Keywords:    []string{"terraform"},
		Safe: []packs.Rule{
			{ID: "terraform-plan-safe", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "plan"))},
			{ID: "terraform-validate-safe", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "validate"))},
			{ID: "terraform-fmt-safe", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "fmt"))},
		},
		Destructive: []packs.Rule{
			{ID: "terraform-destroy-auto-approve", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "terraform destroy with auto-approve destroys infrastructure without confirmation", Remediation: "Remove auto-approve and verify target workspace", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "destroy"), packs.RawTextContains("-auto-approve"))},
			{ID: "terraform-destroy", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "terraform destroy tears down provisioned infrastructure", Remediation: "Confirm environment and plan before destroy", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "destroy"), packs.Not(packs.RawTextContains("-auto-approve")))},
			{ID: "terraform-apply-auto-approve", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "terraform apply auto-approve mutates infrastructure without confirmation", Remediation: "Run plan and require manual approval", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "apply"), packs.RawTextContains("-auto-approve"))},
			{ID: "terraform-apply", Severity: sevMedium, Confidence: confMedium, EnvSensitive: true, Reason: "terraform apply mutates infrastructure state", Remediation: "Review execution plan before apply", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "apply"), packs.Not(packs.RawTextContains("-auto-approve")))},
		},
	}
}
