package kubernetes

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

func kubectlPack() packs.Pack {
	return packs.Pack{
		ID:          "kubernetes.kubectl",
		Name:        "kubectl",
		Description: "Kubernetes kubectl destructive cluster operations",
		Keywords:    []string{"kubectl"},
		Safe: []packs.Rule{
			{ID: "kubectl-get-safe", Match: packs.And(packs.Name("kubectl"), packs.ArgAt(0, "get"))},
			{ID: "kubectl-describe-safe", Match: packs.And(packs.Name("kubectl"), packs.ArgAt(0, "describe"))},
			{ID: "kubectl-logs-safe", Match: packs.And(packs.Name("kubectl"), packs.ArgAt(0, "logs"))},
			{ID: "kubectl-top-safe", Match: packs.And(packs.Name("kubectl"), packs.ArgAt(0, "top"))},
		},
		Destructive: []packs.Rule{
			{
				ID:           "kubectl-delete-namespace",
				Severity:     sevCritical,
				Confidence:   confHigh,
				EnvSensitive: true,
				Reason:       "Deleting a namespace removes all resources contained within it",
				Remediation:  "Delete specific resources instead of deleting the namespace",
				Match: packs.And(
					packs.Name("kubectl"),
					packs.ArgAt(0, "delete"),
					packs.Or(packs.ArgAt(1, "namespace"), packs.ArgAt(1, "namespaces")),
				),
			},
			{
				ID:           "kubectl-delete-workload",
				Severity:     sevHigh,
				Confidence:   confHigh,
				EnvSensitive: true,
				Reason:       "Deleting high-impact Kubernetes resources can cause downtime or data loss",
				Remediation:  "Use rollout or scale commands instead of deleting workloads",
				Match: packs.And(
					packs.Name("kubectl"),
					packs.ArgAt(0, "delete"),
					packs.Or(
						packs.ArgAt(1, "deployment"), packs.ArgAt(1, "deployments"),
						packs.ArgAt(1, "statefulset"), packs.ArgAt(1, "statefulsets"),
						packs.ArgAt(1, "pvc"), packs.ArgAt(1, "pv"),
						packs.ArgAt(1, "node"), packs.ArgAt(1, "nodes"),
						packs.ArgAt(1, "service"), packs.ArgAt(1, "services"),
						packs.ArgAt(1, "secret"), packs.ArgAt(1, "secrets"),
					),
				),
			},
			{
				ID:           "kubectl-delete-resource",
				Severity:     sevMedium,
				Confidence:   confMedium,
				EnvSensitive: true,
				Reason:       "kubectl delete removes targeted resources from the cluster",
				Remediation:  "Use apply and rollout commands instead of delete",
				Match: packs.And(
					packs.Name("kubectl"),
					packs.ArgAt(0, "delete"),
				),
			},
			{
				ID:           "kubectl-drain",
				Severity:     sevHigh,
				Confidence:   confHigh,
				EnvSensitive: true,
				Reason:       "kubectl drain evicts workloads from a node and can disrupt running services",
				Remediation:  "Use cordon to stop new scheduling without evicting workloads",
				Match: packs.And(
					packs.Name("kubectl"),
					packs.ArgAt(0, "drain"),
				),
			},
		},
	}
}
