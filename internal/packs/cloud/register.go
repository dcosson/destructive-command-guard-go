package cloud

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func init() {
	packs.DefaultRegistry.Register(awsPack(), gcpPack(), azurePack(), cloudformationPack())
}
