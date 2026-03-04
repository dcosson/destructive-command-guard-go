package infrastructure

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func ansiblePack() packs.Pack {
	return packs.Pack{
		ID:          "infrastructure.ansible",
		Name:        "Ansible",
		Description: "Ansible destructive module operations",
		Keywords:    []string{"ansible", "ansible-playbook"},
		Safe: []packs.Rule{
			{ID: "ansible-ping-safe", Match: packs.And(packs.Name("ansible"), packs.Flags("-m"), packs.ArgContains("ping"))},
		},
		Destructive: []packs.Rule{
			{ID: "ansible-delete", Severity: sevHigh, Confidence: confHigh, EnvSensitive: true, Reason: "Ansible command uses absent state to remove managed resources", Remediation: "Verify target hosts and desired state before execution", Match: packs.And(
				packs.Or(packs.Name("ansible"), packs.Name("ansible-playbook")),
				packs.RawTextContains("state=absent"),
			)},
		},
	}
}
