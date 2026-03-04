//go:build darwin

package macos

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

func init() {
	packs.DefaultRegistry.Register(communicationPack(), privacyPack(), systemPack())
}
