package guard

import "github.com/dcosson/destructive-command-guard-go/internal/evalcore"

// AllowAllPolicy allows everything regardless of severity.
func AllowAllPolicy() Policy { return evalcore.AllowAllPolicy() }

// PermissivePolicy allows up to High, denies Critical.
func PermissivePolicy() Policy { return evalcore.PermissivePolicy() }

// ModeratePolicy allows up to Medium, denies High+ and Indeterminate.
func ModeratePolicy() Policy { return evalcore.ModeratePolicy() }

// StrictPolicy allows only Low, denies everything else including Indeterminate.
func StrictPolicy() Policy { return evalcore.StrictPolicy() }

// BlockAllPolicy denies everything regardless of severity.
func BlockAllPolicy() Policy { return evalcore.BlockAllPolicy() }

// InteractivePolicy asks the user for Indeterminate, Medium, and High. Allows Low. Denies Critical.
func InteractivePolicy() Policy { return evalcore.InteractivePolicy() }
