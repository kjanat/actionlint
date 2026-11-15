package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// EmptyTriggersRule is the single definition of the empty-triggers rule.
var EmptyTriggersRule = &define.Rule{Name: "empty-triggers", Description: "Report workflows that declare no triggers", Category: "suspicious", Subcategory: "triggers", Recommended: true, Explanation: "Report workflows whose on section is empty. GitHub accepts the workflow, but no event ever starts it.", Help: "Add the events that should start the workflow, or delete the workflow.", Examples: []rule.Example{{
	Description: "Declare the events that start the workflow.",
	Before:      "on:\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
	After:       "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
}},
	Checks: define.Fixed(define.Checks{Workflow: EmptyTriggers}),
}
