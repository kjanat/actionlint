package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// EventsRule is the single definition of the events rule.
var EventsRule = &define.Rule{Name: "events", Description: "Validate workflow triggers and their options", Category: "correctness", Subcategory: "triggers", Recommended: true, Help: "Check the trigger name, activity type and filters at the reported location.",
	Checks: define.Fixed(define.Checks{Workflow: Events}),
}
