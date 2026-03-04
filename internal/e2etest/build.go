package e2etest

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// buildTestBinary builds a package to a temp binary path for subprocess tests.
func buildTestBinary(pkgDir string, outDir string, name string) (string, error) {
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	outPath := filepath.Join(outDir, binName)
	cmd := exec.Command("go", "build", "-o", outPath, pkgDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build failed: %w: %s", err, string(out))
	}
	return outPath, nil
}
