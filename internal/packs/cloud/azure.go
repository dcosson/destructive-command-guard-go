package cloud

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func azurePack() packs.Pack {
	return packs.Pack{
		ID:          "cloud.azure",
		Name:        "Azure CLI",
		Description: "Azure CLI destructive operations",
		Keywords:    []string{"az", "azure"},
		Safe: []packs.Rule{
			{ID: "azure-list-safe", Match: packs.And(packs.Name("az"), packs.ArgContains("list"))},
		},
		Destructive: []packs.Rule{
			{ID: "azure-group-delete", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "az group delete removes all resources in a resource group", Remediation: "Verify group name and subscriptions before deletion", Match: packs.And(packs.Name("az"), packs.ArgAt(0, "group"), packs.ArgAt(1, "delete"))},
		},
	}
}
