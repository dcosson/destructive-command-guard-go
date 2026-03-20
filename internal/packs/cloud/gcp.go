package cloud

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func gcpPack() packs.Pack {
	return packs.Pack{
		ID:          "cloud.gcp",
		Name:        "GCP CLI",
		Description: "Google Cloud CLI destructive operations",
		Keywords:    []string{"gcloud", "gsutil"},
		Safe: []packs.Rule{
			{ID: "gcp-list-safe", Match: packs.And(packs.Name("gcloud"), packs.ArgContains("list"))},
		},
		Rules: []packs.Rule{
			{ID: "gcp-project-delete", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "gcloud projects delete removes the project and all project resources", Remediation: "Delete specific resources instead of deleting the project", Match: packs.And(packs.Name("gcloud"), packs.ArgSubsequence("projects", "delete"))},
		},
	}
}
