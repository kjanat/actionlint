package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var InlineSuppressionRule = &define.Rule{
	Name:        "inline-suppression",
	Description: "Validate inline suppression directives",
	Category:    "correctness",
	Subcategory: "suppressions",
	Recommended: true,
	Help:        "Use a known rule name and put the directive on the declaration it suppresses.",
	Scope:       rule.SourceConfiguration,
	DeclaredBy:  "suppression policy",
}
