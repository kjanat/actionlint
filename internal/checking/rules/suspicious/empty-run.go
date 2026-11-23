package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var EmptyRunRule = &define.Rule{
	Name:        "empty-run",
	Description: "Report steps with an empty run script",
	Category:    "suspicious",
	Subcategory: "steps",
	LegacyGroup: "style",
	Recommended: true,
	Explanation: "Report run steps with an empty script.",
	Help:        "Add commands, or remove the step.",
	Examples: []rule.Example{rule.WorkflowExample("",
		"    steps:\n      - run:\n", "    steps:\n      - run: echo ok\n")},
	Checks: define.Fixed(define.Checks{Step: EmptyRun}),
}

func EmptyRun(step workflows.BoundStep) []reporting.StepFinding {
	run, ok := step.Run()
	if !ok || run.Run == nil || !run.Run.SourceScalar.HasSource || run.Run.Value != "" {
		return nil
	}
	finding := reporting.Warning("empty-run", "", run.RunPos, "step runs an empty script, so it does nothing")
	finding.Step = step
	return []reporting.StepFinding{finding}
}
