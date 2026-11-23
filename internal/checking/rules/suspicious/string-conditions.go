package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var StringConditionsRule = &define.Rule{
	Name:            "string-conditions",
	Description:     "Flag string conditions whose truthiness may be misleading",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Help:            "Use a boolean expression. Any nonempty string is truthy, including the string false.",
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	DeclaredBy:      "expression",
}
