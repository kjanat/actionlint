package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyTriggersRule is the single definition of the empty-triggers rule.
var EmptyTriggersRule = &define.Rule{
	Name:        "empty-triggers",
	Description: "Report workflows that declare no triggers",
	Category:    "suspicious",
	Subcategory: "triggers",
	LegacyGroup: "style",
	Recommended: true,
	Explanation: "Report workflows whose on section is empty. GitHub accepts the workflow, but no event ever starts it.",
	Help:        "Add the events that should start the workflow, or delete the workflow.",
	Examples: []rule.Example{{
		Description: "Declare the events that start the workflow.",
		Before:      "on:\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
		After:       "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
	}},
	Checks: define.Fixed(define.Checks{Workflow: EmptyTriggers}),
}

// EmptyTriggers reports an "on" section that declares no events.
func EmptyTriggers(w declaration.Workflow) []reporting.StepFinding {
	if !w.Present() || w.OnPos == nil || w.On == nil || len(w.On) > 0 {
		return nil
	}
	return []reporting.StepFinding{reporting.Warning("empty-triggers", "", w.OnPos, "workflow declares no triggers in \"on\", so no event ever starts it")}
}
