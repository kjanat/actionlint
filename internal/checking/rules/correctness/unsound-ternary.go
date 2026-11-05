package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// UnsoundTernaryDeclaration describes the unsound-ternary rule.
func UnsoundTernaryDeclaration() rule.Declaration {
	return rule.Declaration{Name: "unsound-ternary", Description: "Report a && b || c expressions where b is always falsy", Category: "correctness", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report a && b || c expressions where b is always falsy.", Help: "The middle operand of a && b || c must be truthy to select b; use an explicit conditional or a truthy representation.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#unsound-ternary"}
}
