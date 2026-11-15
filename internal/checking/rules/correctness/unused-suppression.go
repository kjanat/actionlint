package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// UnusedSuppressionRule is the single definition of the unused-suppression rule.
var UnusedSuppressionRule = &define.Rule{Name: "unused-suppression", Description: "Report unused inline suppression selectors", Category: "correctness", Subcategory: "suppressions", Recommended: true, Explanation: "Report each inline suppression selector that matches no enabled findings in the checked YAML declaration. Information by default; errors with --strict. A selector used by any analyzed invocation of the same source is considered used. Incomplete analyses do not establish that a selector is unused.", Help: "Run actionlint fix --apply or use the editor quick fix to remove unused selectors. Selectors that suppress findings and the comment's reason are preserved. A suppression comment is removed when all its selectors are unused. Configure lint.rules.correctness.unused-suppression to change its level.", SupportsActions: true, Scope: rule.SourceConfiguration, DefaultLevel: "info",
	DeclaredBy: "suppression policy",
}
