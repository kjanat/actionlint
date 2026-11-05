package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// UnusedSuppressionDeclaration describes the unused-suppression rule.
func UnusedSuppressionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "unused-suppression", Description: "Report unused inline suppression selectors", Category: "correctness", Subcategory: "suppressions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report each inline suppression selector that matches no enabled findings in the checked YAML declaration. Information by default; errors with --strict. A selector used by any analyzed invocation of the same source is considered used. Incomplete analyses do not establish that a selector is unused.", Help: "Run actionlint fix --apply or use the editor quick fix to remove unused selectors. Selectors that suppress findings and the comment's reason are preserved. A suppression comment is removed when all its selectors are unused. Configure lint.rules.correctness.unused-suppression to change its level.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#unused-suppression", SupportsActions: true, Scope: rule.SourceConfiguration, DefaultLevel: "info"}
}
