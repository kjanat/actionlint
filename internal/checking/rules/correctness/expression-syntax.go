package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// ExpressionSyntaxRule is the single definition of the expression-syntax rule.
var ExpressionSyntaxRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "expression-syntax",
	Description:      "Parse expression tokens and grammar",
	Category:         "correctness",
	Subcategory:      "expressions",
	Help:             "Correct the indicated expression token, literal or delimiter.",
	Recommended:      true,
	SupportsActions:  true,
	DeclaredBy:       "expression",
}
