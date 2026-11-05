package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ExecutableBitDeclaration describes the executable-bit rule.
func ExecutableBitDeclaration() rule.Declaration {
	return rule.Declaration{Name: "executable-bit", Description: "Report directly invoked repository scripts not marked executable in Git", Category: "correctness", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report directly invoked repository scripts not marked executable in Git.", Help: "Record the executable bit for the referenced script in Git, or invoke it explicitly through its interpreter.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#executable-bit"}
}
