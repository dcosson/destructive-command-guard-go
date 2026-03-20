package containers

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func composePack() packs.Pack {
	return packs.Pack{
		ID:          "containers.compose",
		Name:        "Docker Compose",
		Description: "Docker Compose destructive operations",
		Keywords:    []string{"docker-compose", "compose", "docker"},
		Safe: []packs.Rule{
			{ID: "compose-ps-safe", Match: packs.Or(
				packs.And(packs.Name("docker-compose"), packs.ArgAt(0, "ps")),
				packs.And(packs.Name("docker"), packs.ArgAt(0, "compose"), packs.ArgAt(1, "ps")),
			)},
			{ID: "compose-logs-safe", Match: packs.Or(
				packs.And(packs.Name("docker-compose"), packs.ArgAt(0, "logs")),
				packs.And(packs.Name("docker"), packs.ArgAt(0, "compose"), packs.ArgAt(1, "logs")),
			)},
		},
		Rules: []packs.Rule{
			{
				ID:          "compose-down-volumes",
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "Compose down, rm, and stop tear down service containers and attached resources",
				Remediation: "Use compose restart for service restarts and avoid volume deletion flags",
				Match: packs.Or(
					packs.And(
						packs.Name("docker-compose"),
						packs.Or(
							packs.And(packs.ArgAt(0, "down"), packs.Or(packs.Flags("-v"), packs.Flags("--volumes"))),
							packs.And(packs.ArgAt(0, "rm"), packs.Or(packs.Flags("-f"), packs.Flags("--force"))),
							packs.ArgAt(0, "stop"),
						),
					),
					packs.And(
						packs.Name("docker"),
						packs.ArgAt(0, "compose"),
						packs.Or(
							packs.And(packs.ArgAt(1, "down"), packs.Or(packs.Flags("-v"), packs.Flags("--volumes"))),
							packs.And(packs.ArgAt(1, "rm"), packs.Or(packs.Flags("-f"), packs.Flags("--force"))),
							packs.ArgAt(1, "stop"),
						),
					),
				),
			},
		},
	}
}
