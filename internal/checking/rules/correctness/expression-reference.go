package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// ExpressionReferenceRule is the single definition of the expression-reference rule.
var ExpressionReferenceRule = &define.Rule{Name: "expression-reference", Description: "Resolve expression variables, properties and functions", Category: "correctness", Subcategory: "expressions", Help: "Use a declared context, property or function; check the suggested spelling and available names.", Recommended: true, SupportsActions: true,
	DeclaredBy: "expression",
}
