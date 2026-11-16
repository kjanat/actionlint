package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
)

// StringConditionsRule is the single definition of the string-conditions rule.
var StringConditionsRule = &define.Rule{
	Name:            "string-conditions",
	Description:     "Flag string conditions whose truthiness may be misleading",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Help:            "Use a boolean expression; a nonempty string, including the string false, is truthy.",
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	DeclaredBy:      "expression",
}
