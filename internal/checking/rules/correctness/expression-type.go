package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// ExpressionTypeRule is the single definition of the expression-type rule.
var ExpressionTypeRule = &define.Rule{
	Name:            "expression-type",
	Description:     "Validate expression operand and argument types",
	Category:        "correctness",
	Subcategory:     "expressions",
	Help:            "Supply the expected value type; convert serialized values explicitly when needed.",
	Recommended:     true,
	SupportsActions: true,
	DeclaredBy:      "expression",
}
