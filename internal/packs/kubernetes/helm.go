package kubernetes

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func helmPack() packs.Pack {
	return packs.Pack{
		ID:          "kubernetes.helm",
		Name:        "Helm",
		Description: "Helm destructive release operations",
		Keywords:    []string{"helm"},
		Safe: []packs.Rule{
			{ID: "helm-list-safe", Match: packs.And(packs.Name("helm"), packs.ArgAt(0, "list"))},
			{ID: "helm-status-safe", Match: packs.And(packs.Name("helm"), packs.ArgAt(0, "status"))},
			{ID: "helm-get-safe", Match: packs.And(packs.Name("helm"), packs.ArgAt(0, "get"))},
			{ID: "helm-template-safe", Match: packs.And(packs.Name("helm"), packs.ArgAt(0, "template"))},
		},
		Rules: []packs.Rule{
			{
				ID:           "helm-uninstall",
				Severity:     sevHigh,
				Confidence:   confHigh,
				EnvSensitive: true,
				Reason:       "helm uninstall removes a release and its managed Kubernetes resources",
				Remediation:  "Use helm upgrade to change release state without uninstalling",
				Match: packs.And(
					packs.Name("helm"),
					packs.Or(packs.ArgAt(0, "uninstall"), packs.ArgAt(0, "delete")),
				),
			},
			{
				ID:           "helm-rollback",
				Severity:     sevMedium,
				Confidence:   confHigh,
				EnvSensitive: true,
				Reason:       "helm rollback switches live release state to an earlier revision",
				Remediation:  "Use helm upgrade to apply forward-only changes",
				Match: packs.And(
					packs.Name("helm"),
					packs.ArgAt(0, "rollback"),
				),
			},
		},
	}
}
