package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// RunnerLabelDeclaration describes the runner-label rule.
func RunnerLabelDeclaration() rule.Declaration {
	return rule.Declaration{Name: "runner-label", Description: "Report unknown or incompatible runner labels", Category: "correctness", Subcategory: "runners", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report unknown or incompatible runner labels.", Help: "Correct the runner label or declare repository-specific labels in repository.runners.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#runner-label"}
}
