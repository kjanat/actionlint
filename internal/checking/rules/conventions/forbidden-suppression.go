package conventions

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
)

// ForbiddenSuppressionRule is the single definition of the forbidden-suppression rule.
var ForbiddenSuppressionRule = &define.Rule{
	Name:        "forbidden-suppression",
	Aliases:     []rule.Alias{{Name: "disallow-suppressions", Deprecated: true}},
	Description: "Report suppressions forbidden by policy",
	Category:    "conventions",
	Subcategory: "suppressions",
	LegacyGroup: "policy",
	Policy:      "disallow-suppressions",
	Help:        "Remove the suppression and address its finding, or change the repository suppression policy.",
	Scope:       rule.SourceConfiguration,
	Options:     define.Options[configuration.SuppressionsPolicy](),
	DeclaredBy:  "suppression policy",
}
