package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var ContextAvailabilityRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "context-availability",
	Description:      "Validate contexts and functions available at a workflow field",
	Category:         "correctness",
	Subcategory:      "contexts",
	Help:             "Use contexts and functions that this field allows, or move the expression.",
	Recommended:      true,
	SupportsActions:  true,
	DeclaredBy:       "expression",
}
