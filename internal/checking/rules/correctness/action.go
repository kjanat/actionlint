package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ActionDeclaration describes the action rule.
func ActionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "action", Description: "Validate action references, inputs, outputs, and local metadata", Category: "correctness", Subcategory: "actions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate action references, inputs, outputs, and local metadata.", Help: "Check the action reference, runtime, declared inputs and action metadata at the indicated source.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#action", SupportsActions: true}
}
