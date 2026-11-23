package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var EventPayloadRule = &define.Rule{
	Name:            "event-payload",
	Description:     "Validate properties against the configured event payloads",
	Category:        "correctness",
	Subcategory:     "events",
	Help:            "Use a property that the declared events and activity types provide.",
	Recommended:     true,
	SupportsActions: true,
	DeclaredBy:      "expression",
}
