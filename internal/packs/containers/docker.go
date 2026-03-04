package containers

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

func dockerPack() packs.Pack {
	return packs.Pack{
		ID:          "containers.docker",
		Name:        "Docker",
		Description: "Docker container, image, volume, and network destructive operations",
		Keywords:    []string{"docker"},
		Safe: []packs.Rule{
			{ID: "docker-ps-safe", Match: packs.And(packs.Name("docker"), packs.ArgAt(0, "ps"))},
			{ID: "docker-images-safe", Match: packs.And(packs.Name("docker"), packs.ArgAt(0, "images"))},
			{ID: "docker-inspect-safe", Match: packs.And(packs.Name("docker"), packs.ArgAt(0, "inspect"))},
			{ID: "docker-logs-safe", Match: packs.And(packs.Name("docker"), packs.ArgAt(0, "logs"))},
			{ID: "docker-build-safe", Match: packs.And(packs.Name("docker"), packs.ArgAt(0, "build"))},
			{ID: "docker-pull-push-safe", Match: packs.And(packs.Name("docker"), packs.Or(packs.ArgAt(0, "pull"), packs.ArgAt(0, "push")))},
			{ID: "docker-run-exec-safe", Match: packs.And(packs.Name("docker"), packs.Or(packs.ArgAt(0, "run"), packs.ArgAt(0, "exec")))},
		},
		Destructive: []packs.Rule{
			{
				ID:          "docker-system-prune",
				Severity:    sevHigh,
				Confidence:  confHigh,
				Reason:      "Docker resource removal and prune operations delete containers, images, networks, or cached data",
				Remediation: "Use docker ps/images to inspect targets first and prefer scoped cleanup commands",
				Match: packs.And(
					packs.Name("docker"),
					packs.Not(packs.ArgAt(0, "compose")),
					packs.Or(
						packs.And(packs.ArgAt(0, "system"), packs.ArgAt(1, "prune")),
						packs.And(packs.ArgAt(0, "builder"), packs.ArgAt(1, "prune")),
						packs.ArgAt(0, "rm"),
						packs.And(packs.ArgAt(0, "container"), packs.ArgAt(1, "rm")),
						packs.ArgAt(0, "rmi"),
						packs.And(packs.ArgAt(0, "image"), packs.ArgAt(1, "rm")),
						packs.ArgAt(0, "stop"),
						packs.And(packs.ArgAt(0, "container"), packs.ArgAt(1, "stop")),
						packs.ArgAt(0, "kill"),
						packs.And(packs.ArgAt(0, "container"), packs.ArgAt(1, "kill")),
						packs.And(packs.ArgAt(0, "volume"), packs.ArgAt(1, "rm")),
						packs.And(packs.ArgAt(0, "network"), packs.ArgAt(1, "rm")),
					),
				),
			},
		},
	}
}
