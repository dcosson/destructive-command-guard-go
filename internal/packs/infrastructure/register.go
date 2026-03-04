package infrastructure

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func init() {
	packs.DefaultRegistry.Register(terraformPack(), pulumiPack(), ansiblePack())
}
