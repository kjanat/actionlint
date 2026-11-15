package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
)

// ActionRule is the single definition of the action rule.
var ActionRule = &define.Rule{Name: "action", Description: "Validate action references, inputs, outputs, and local metadata", Category: "correctness", Subcategory: "actions", Recommended: true, Help: "Check the action reference, runtime, declared inputs and action metadata at the indicated source.", SupportsActions: true,
	Walker: func(c define.Inputs, base workflowvisit.Base) workflowvisit.Rule {
		return NewRuleAction(c.Context, base, c.Actions.Prepared(), c.RemoteActions).WithLifecycle(c.Lifecycle)
	},
}
