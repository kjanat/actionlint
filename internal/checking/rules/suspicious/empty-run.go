package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyRunRule is the single definition of the empty-run rule.
var EmptyRunRule = &define.Rule{
	Name:        "empty-run",
	Description: "Report steps with an empty run script",
	Category:    "suspicious",
	Subcategory: "steps",
	LegacyGroup: "style",
	Recommended: true,
	Explanation: "Report run steps whose script is empty. GitHub accepts the step, but it runs nothing.",
	Help:        "Write the commands the step should run, or remove the step.",
	Examples: []rule.Example{rule.WorkflowExample("Write the commands the step runs.",
		"    steps:\n      - run:\n", "    steps:\n      - run: echo ok\n")},
	Checks: define.Fixed(define.Checks{Step: EmptyRun}),
}

// EmptyRun reports a run step whose script is empty.
func EmptyRun(step declaration.Step) []reporting.StepFinding {
	run, ok := step.Run()
	// Only a string run value carries a source scalar; other kinds already
	// have a syntax-check finding.
	if !ok || run.Run == nil || !run.Run.SourceScalar.HasSource || run.Run.Value != "" {
		return nil
	}
	finding := reporting.Warning("empty-run", "", run.RunPos, "step runs an empty script, so it does nothing")
	finding.Step = step
	return []reporting.StepFinding{finding}
}
