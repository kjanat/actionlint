package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var UnusedSuppressionRule = &define.Rule{
	Name:            "unused-suppression",
	Description:     "Report unused inline suppression selectors",
	Category:        "correctness",
	Subcategory:     "suppressions",
	Recommended:     true,
	Explanation:     "Report inline suppression selectors that match no enabled finding.",
	Help:            "Run actionlint fix --apply or the editor quick fix. The fix keeps used selectors and the reason, and deletes a comment when none of its selectors are used.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	DefaultLevel:    "info",
	DeclaredBy:      "suppression policy",
}
