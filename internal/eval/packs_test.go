package eval

// Blank imports to register packs into DefaultRegistry for tests.
import (
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/cloud"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/containers"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/core"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/database"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/frameworks"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/infrastructure"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/kubernetes"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/macos"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/personal"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/platform"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/remote"
	_ "github.com/dcosson/destructive-command-guard-go/internal/packs/secrets"
)
