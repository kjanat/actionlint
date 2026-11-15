package registration

import (
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// ruleSpec keeps each native declaration and its optional execution together.
type ruleSpec struct {
	declaration rule.Declaration
	behavior    implementation
}

func newSpec(declaration rule.Declaration, behavior implementation) ruleSpec {
	return ruleSpec{declaration: declaration, behavior: behavior}
}

func declarationOnly(declaration rule.Declaration) ruleSpec {
	return newSpec(declaration, implementation{})
}

func declarationSpec(check checkinputs.DeclarationCheck) ruleSpec {
	return newSpec(check.Declaration, declarationImplementation(check))
}

// Declarations returns detached native declarations in presentation order.
func Declarations() []rule.Declaration {
	catalog := nativeRuleCatalog()
	out := make([]rule.Declaration, len(catalog))
	for i, spec := range catalog {
		out[i] = spec.declaration
		out[i].Descriptor = out[i].Clone()
	}
	return out
}
