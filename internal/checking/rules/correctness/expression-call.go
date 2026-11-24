package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var ExpressionCallRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "expression-call",
	Description:      "Validate function invocation and constant arguments",
	Category:         "correctness",
	Subcategory:      "expressions",
	Help:             "Check parentheses, argument count, format placeholders and literal JSON.",
	Recommended:      true,
	SupportsActions:  true,
	DeclaredBy:       "expression",
}
