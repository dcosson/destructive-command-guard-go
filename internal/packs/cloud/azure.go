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
			{ID: "azure-group-delete", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "az group delete removes the resource group and all contained resources", Remediation: "Delete specific resources instead of deleting the group", Match: packs.And(packs.Name("az"), packs.ArgAt(0, "group"), packs.ArgAt(1, "delete"))},
		},
	}
}
