package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var ExpressionTypeRule = &define.Rule{
	Name:            "expression-type",
	Description:     "Validate expression operand and argument types",
	Category:        "correctness",
	Subcategory:     "expressions",
	Help:            "Pass the expected type, or convert the value explicitly.",
	Recommended:     true,
	SupportsActions: true,
	DeclaredBy:      "expression",
}
