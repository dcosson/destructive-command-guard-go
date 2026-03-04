package guard

import "github.com/dcosson/destructive-command-guard-go/internal/evalcore"

// StrictPolicy denies Medium+ and Indeterminate.
func StrictPolicy() Policy { return evalcore.StrictPolicy() }

// InteractivePolicy asks on Medium and Indeterminate, denies High+.
func InteractivePolicy() Policy { return evalcore.InteractivePolicy() }

// PermissivePolicy denies Critical and asks on High.
func PermissivePolicy() Policy { return evalcore.PermissivePolicy() }
