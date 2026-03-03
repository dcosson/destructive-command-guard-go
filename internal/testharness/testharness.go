package testharness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dcosson/destructive-command-guard-go/guard"
)

// HasRegisteredPack reports whether a pack ID is currently registered.
func HasRegisteredPack(packID string) bool {
	for _, p := range guard.Packs() {
		if p.ID == packID {
			return true
		}
	}
	return false
}

// SkipIfPackMissing skips the calling test if the named pack is absent.
func SkipIfPackMissing(t testing.TB, packID string) {
	t.Helper()
	if HasRegisteredPack(packID) {
		return
	}
	t.Skipf("pack %s not registered", packID)
}

// FindModuleRoot finds the nearest directory containing go.mod.
func FindModuleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
