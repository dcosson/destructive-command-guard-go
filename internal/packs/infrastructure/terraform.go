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
			{ID: "terraform-destroy-auto-approve", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "terraform destroy -auto-approve deletes provisioned infrastructure immediately", Remediation: "Use targeted resource updates instead of destroy", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "destroy"), packs.RawTextContains("-auto-approve"))},
			{ID: "terraform-destroy", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "terraform destroy deletes provisioned infrastructure", Remediation: "Use targeted resource updates instead of destroy", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "destroy"), packs.Not(packs.RawTextContains("-auto-approve")))},
			{ID: "terraform-apply-auto-approve", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "terraform apply -auto-approve mutates infrastructure immediately", Remediation: "Use terraform plan for read-only change output", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "apply"), packs.RawTextContains("-auto-approve"))},
			{ID: "terraform-apply", Severity: sevMedium, Confidence: confMedium, EnvSensitive: true, Reason: "terraform apply mutates infrastructure state", Remediation: "Use terraform plan for read-only change output", Match: packs.And(packs.Name("terraform"), packs.ArgAt(0, "apply"), packs.Not(packs.RawTextContains("-auto-approve")))},
		},
	}
}
