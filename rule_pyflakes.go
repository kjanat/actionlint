package actionlint

// RulePyflakes preserves the retired Python checker's Go API.
// Its visitor callbacks perform no analysis.
//
// Deprecated: Pyflakes integration was removed. This rule is inert.
type RulePyflakes struct {
	RuleBase
}

// NewRulePyflakes creates an inert compatibility rule. Both parameters are ignored;
// the executable is never resolved or invoked, and proc may be nil.
//
// Deprecated: Pyflakes integration was removed. This constructor returns a no-op rule.
func NewRulePyflakes(executable string, proc *concurrentProcess) (*RulePyflakes, error) {
	return &RulePyflakes{
		RuleBase: NewRuleBase("pyflakes", "Deprecated Python checker; performs no analysis"),
	}, nil
}
