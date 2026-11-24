package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var ExpressionSyntaxRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "expression-syntax",
	Description:      "Parse expression tokens and grammar",
	Category:         "correctness",
	Subcategory:      "expressions",
	Help:             "Fix the reported token, literal or delimiter.",
	Recommended:      true,
	SupportsActions:  true,
	DeclaredBy:       "expression",
}
