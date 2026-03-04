package evalcore

// StrictPolicy denies Medium+ and Indeterminate.
func StrictPolicy() Policy { return strictPolicy{} }

type strictPolicy struct{}

func (strictPolicy) Decide(a Assessment) Decision {
	if a.Severity >= Medium || a.Severity == Indeterminate {
		return Deny
	}
	return Allow
}

// InteractivePolicy asks on Medium and Indeterminate, denies High+.
func InteractivePolicy() Policy { return interactivePolicy{} }

type interactivePolicy struct{}

func (interactivePolicy) Decide(a Assessment) Decision {
	switch {
	case a.Severity >= High:
		return Deny
	case a.Severity == Medium || a.Severity == Indeterminate:
		return Ask
	default:
		return Allow
	}
}

// PermissivePolicy denies Critical and asks on High.
func PermissivePolicy() Policy { return permissivePolicy{} }

type permissivePolicy struct{}

func (permissivePolicy) Decide(a Assessment) Decision {
	switch {
	case a.Severity == Critical:
		return Deny
	case a.Severity == High:
		return Ask
	default:
		return Allow
	}
}
