package eval

type interactivePolicy struct{}

func (interactivePolicy) Decide(a Assessment) Decision {
	switch {
	case a.Severity >= SeverityHigh:
		return DecisionDeny
	case a.Severity == SeverityMedium || a.Severity == SeverityIndeterminate:
		return DecisionAsk
	default:
		return DecisionAllow
	}
}

type strictPolicy struct{}

func (strictPolicy) Decide(a Assessment) Decision {
	if a.Severity >= SeverityMedium || a.Severity == SeverityIndeterminate {
		return DecisionDeny
	}
	return DecisionAllow
}

type permissivePolicy struct{}

func (permissivePolicy) Decide(a Assessment) Decision {
	switch {
	case a.Severity == SeverityCritical:
		return DecisionDeny
	case a.Severity == SeverityHigh:
		return DecisionAsk
	default:
		return DecisionAllow
	}
}
