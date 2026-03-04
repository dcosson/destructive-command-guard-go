package cloud

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func cloudformationPack() packs.Pack {
	return packs.Pack{
		ID:          "cloud.cloudformation",
		Name:        "AWS CloudFormation",
		Description: "CloudFormation stack destructive operations",
		Keywords:    []string{"aws", "cloudformation"},
		Safe: []packs.Rule{
			{ID: "cloudformation-describe-safe", Match: packs.And(packs.Name("aws"), packs.ArgAt(0, "cloudformation"), packs.ArgAt(1, "describe-stacks"))},
		},
		Destructive: []packs.Rule{
			{ID: "cloudformation-delete-stack", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "delete-stack removes CloudFormation-managed resources", Remediation: "Review stack impact before deletion", Match: packs.And(packs.Name("aws"), packs.ArgAt(0, "cloudformation"), packs.ArgAt(1, "delete-stack"))},
		},
	}
}
