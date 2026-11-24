package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var EmptyTriggersRule = &define.Rule{
	Name:        "empty-triggers",
	Description: "Report workflows that declare no triggers",
	Category:    "suspicious",
	Subcategory: "triggers",
	LegacyGroup: "style",
	Recommended: true,
	Explanation: "Report workflows with an empty on section. No event starts them.",
	Help:        "Add trigger events, or delete the workflow.",
	Examples: []rule.Example{{
		Before: "on:\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
		After:  "on: push\npermissions: {}\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n",
	}},
	Checks: define.Fixed(define.Checks{Workflow: EmptyTriggers}),
}

func EmptyTriggers(w workflows.BoundWorkflow) []reporting.StepFinding {
	if !w.Present() || w.OnPos == nil || w.On == nil || len(w.On) > 0 {
		return nil
	}
	return []reporting.StepFinding{reporting.Warning("empty-triggers", "", w.OnPos, "workflow declares no triggers in \"on\", so no event ever starts it")}
}
