package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CoercionDeclaration describes the coercion rule.
func CoercionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "coercion", Aliases: []rule.Alias{{Name: "suspicious-coercion", Deprecated: true}}, Description: "Warn about surprising string and comparison coercions", Category: "suspicious", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Warn when a string parameter converts a boolean or null, when a comparison mixes a collection with a type other than null, or when an ordering comparison uses booleans, null or collections. These expressions are valid in GitHub Actions. Unknown types do not establish a suspicious conversion. Collection equality within the same type is allowed because Actions compares instance identity. Equality with null is a presence check and is allowed.", Help: "Check the intended argument or operand type. Use a string literal when text is intended, or compare values of the intended types. This rule offers no automatic edit because the intended value is unknown.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#coercion", Examples: []rule.Example{rule.WorkflowExample("Use the boolean value directly when checking branch protection.", "    if: startsWith(github.ref_protected, 'true')\n", "    if: github.ref_protected\n")}, SupportsActions: true, Scope: rule.CompositeConfiguration, DefaultLevel: "warn"}
}
