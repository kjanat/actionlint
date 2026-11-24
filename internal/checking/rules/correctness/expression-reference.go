package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var ExpressionReferenceRule = &define.Rule{
	Name:            "expression-reference",
	Description:     "Resolve expression variables, properties and functions",
	Category:        "correctness",
	Subcategory:     "expressions",
	MovedOptions:    []rule.MovedOption{{Option: "variables", Repository: "variables"}, {Option: "secrets", Repository: "secrets"}},
	Help:            "Use a declared context, property or function. Check the suggested spelling.",
	Recommended:     true,
	SupportsActions: true,
	DeclaredBy:      "expression",
}
