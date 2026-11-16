package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// EventPayloadRule is the single definition of the event-payload rule.
var EventPayloadRule = &define.Rule{
	Name:            "event-payload",
	Description:     "Validate properties against the configured event payloads",
	Category:        "correctness",
	Subcategory:     "events",
	Help:            "Use a property supplied by the declared events and activity types. Unknown events and dynamic payload maps remain open.",
	Recommended:     true,
	SupportsActions: true,
	DeclaredBy:      "expression",
}
