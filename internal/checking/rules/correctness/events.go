package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// EventsDeclaration describes the events rule.
func EventsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "events", Description: "Validate workflow triggers and their options", Category: "correctness", Subcategory: "triggers", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate workflow triggers and their options.", Help: "Check the trigger name, activity type and filters at the reported location.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#events"}
}
