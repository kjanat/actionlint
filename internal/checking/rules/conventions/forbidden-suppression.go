package conventions

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var ForbiddenSuppressionRule = &define.Rule{
	Name:        "forbidden-suppression",
	Aliases:     []rule.Alias{{Name: "disallow-suppressions", Deprecated: true}},
	Description: "Report suppressions forbidden by policy",
	Category:    "conventions",
	Subcategory: "suppressions",
	LegacyGroup: "policy",
	Policy:      "disallow-suppressions",
	Help:        "Remove the suppression and fix its finding, or change the suppression policy.",
	Scope:       rule.SourceConfiguration,
	Options:     define.Options[configuration.SuppressionsPolicy](),
	DeclaredBy:  "suppression policy",
}
