package evalcore

// AllowAllPolicy allows everything regardless of severity.
func AllowAllPolicy() Policy { return allowAllPolicy{} }

type allowAllPolicy struct{}

func (allowAllPolicy) Decide(a Assessment) Decision { return Allow }

// PermissivePolicy allows up to High, denies Critical.
func PermissivePolicy() Policy { return permissivePolicy{} }

type permissivePolicy struct{}

func (permissivePolicy) Decide(a Assessment) Decision {
	if a.Severity >= Critical {
		return Deny
	}
	return Allow
}

// ModeratePolicy allows up to Medium, denies High+.
func ModeratePolicy() Policy { return moderatePolicy{} }

type moderatePolicy struct{}

func (moderatePolicy) Decide(a Assessment) Decision {
	if a.Severity >= High || a.Severity == Indeterminate {
		return Deny
	}
	return Allow
}

// StrictPolicy denies everything regardless of severity.
func StrictPolicy() Policy { return strictPolicy{} }

type strictPolicy struct{}

func (strictPolicy) Decide(a Assessment) Decision { return Deny }

// InteractivePolicy asks the user for Indeterminate, Medium, and High.
// Allows Low. Denies Critical.
func InteractivePolicy() Policy { return interactivePolicy{} }

type interactivePolicy struct{}

func (interactivePolicy) Decide(a Assessment) Decision {
	switch {
	case a.Severity == Critical:
		return Deny
	case a.Severity == Low:
		return Allow
	default:
		// Indeterminate, Medium, High → Ask
		return Ask
	}
}
