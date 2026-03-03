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
