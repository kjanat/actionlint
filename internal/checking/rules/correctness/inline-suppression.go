package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// InlineSuppressionRule is the single definition of the inline-suppression rule.
var InlineSuppressionRule = &define.Rule{
	Name:        "inline-suppression",
	Description: "Validate inline suppression directives",
	Category:    "correctness",
	Subcategory: "suppressions",
	Recommended: true,
	Help:        "Use a known rule name and place the directive on the declaration it is intended to suppress.",
	Scope:       rule.SourceConfiguration,
	DeclaredBy:  "suppression policy",
}
