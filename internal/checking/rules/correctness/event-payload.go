package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// EventPayloadDeclaration describes the event-payload rule.
func EventPayloadDeclaration() rule.Declaration {
	return rule.Declaration{Name: "event-payload", Description: "Validate properties against the configured event payloads", Category: "correctness", Subcategory: "events", Maturity: "stable", Provider: "actionlint", Explanation: "Validate properties against the configured event payloads.", Help: "Use a property supplied by the declared events and activity types. Unknown events and dynamic payload maps remain open.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#event-payload", Recommended: true, SupportsActions: true}
}
