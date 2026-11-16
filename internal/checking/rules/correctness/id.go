package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/identifier"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// IdRule is the single definition of the id rule.
var IdRule = &define.Rule{
	Name:        "id",
	Description: "Report invalid job or step IDs and duplicate step IDs",
	Category:    "correctness",
	Subcategory: "identity",
	Recommended: true,
	Help:        "Use a unique valid identifier within its job or workflow scope.",
	Examples: []rule.Example{rule.WorkflowExample("Begin step IDs with a letter or underscore.",
		"    steps:\n      - id: 1build\n        run: echo ok\n", "    steps:\n      - id: build\n        run: echo ok\n")},
	Checks: define.Fixed(define.Checks{Job: checkIdentifiers}),
}

func checkIdentifiers(job workflows.Job) []reporting.StepFinding {
	name := func(value *definition.String) identifier.Name {
		return identifier.Name{Value: value.Value, Dynamic: value.ContainsExpression(), Start: source.ZeroIfNil(value.Pos)}
	}
	input := identifier.JobIdentifiers{}
	if job.ID != nil {
		input.ID = new(name(job.ID))
	}
	for _, value := range job.Needs {
		input.Needs = append(input.Needs, name(value))
	}
	owners := map[source.Position]workflows.Step{}
	for step := range job.AllSteps() {
		if step.ID != nil {
			input.Steps = append(input.Steps, name(step.ID))
			owners[source.ZeroIfNil(source.NilUnlessPresent(step.ID.Position()))] = step
		}
	}
	out := reporting.FromDiagnostics(identifier.Check(input))
	for i := range out {
		out[i].Step = owners[out[i].Start]
	}
	return out
}
