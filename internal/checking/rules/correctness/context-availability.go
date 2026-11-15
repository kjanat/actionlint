package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// ContextAvailabilityRule is the single definition of the context-availability rule.
var ContextAvailabilityRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "context-availability",
	Description:      "Validate contexts and functions available at a workflow field",
	Category:         "correctness",
	Subcategory:      "contexts",
	Help:             "Move the expression to a field where its contexts and functions are available, or use an available value.",
	Recommended:      true,
	SupportsActions:  true,
	DeclaredBy:       "expression",
}
