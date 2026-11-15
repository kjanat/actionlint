package correctness

import (
	"iter"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	identifier "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/id"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func checkIdentifiers(job declaration.Job) []reporting.Finding {
	name := func(value *workflowlang.String) identifier.Name {
		return identifier.Name{Value: value.Value, Dynamic: value.ContainsExpression(), Start: reporting.Position(value.Pos)}
	}
	input := identifier.Job{}
	if job.ID != nil {
		input.ID = new(name(job.ID))
	}
	for _, value := range job.Needs {
		input.Needs = append(input.Needs, name(value))
	}
	owners := map[sourcepos.Position]declaration.Step{}
	var collect func(iter.Seq[declaration.Step])
	collect = func(steps iter.Seq[declaration.Step]) {
		for step := range steps {
			if step.ID != nil {
				input.Steps = append(input.Steps, name(step.ID))
				owners[reporting.Position(reporting.OptionalPosition(step.ID.Position()))] = step
			}
			if parallel, ok := step.ParallelSteps(); ok {
				collect(parallel)
			}
		}
	}
	collect(job.Steps())
	out := reporting.FromDiagnostics(identifier.Check(input))
	for i := range out {
		out[i].Step = owners[out[i].Start]
	}
	return out
}
