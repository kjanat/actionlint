package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ContextAvailabilityDeclaration describes the context-availability rule.
func ContextAvailabilityDeclaration() rule.Declaration {
	return rule.Declaration{ExecutionFailure: true, Name: "context-availability", Description: "Validate contexts and functions available at a workflow field", Category: "correctness", Subcategory: "contexts", Maturity: "stable", Provider: "actionlint", Explanation: "Validate contexts and functions available at a workflow field.", Help: "Move the expression to a field where its contexts and functions are available, or use an available value.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#context-availability", Recommended: true, SupportsActions: true}
}
