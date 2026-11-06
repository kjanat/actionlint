package correctness

import (
	"iter"

	identifier "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/id"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func checkIdentifiers(job declaration.JobView) []reporting.Finding {
	name := func(value declaration.StringView) identifier.Name {
		return identifier.Name{Value: value.Value(), Dynamic: value.ContainsExpression(), Start: reporting.Position(reporting.ViewPosition(value.Pos()))}
	}
	input := identifier.Job{}
	if job.ID().Present() {
		input.ID = new(name(job.ID()))
	}
	for value := range job.Needs() {
		input.Needs = append(input.Needs, name(value))
	}
	owners := map[sourcepos.Position]declaration.StepView{}
	var collect func(iter.Seq[declaration.StepView])
	collect = func(steps iter.Seq[declaration.StepView]) {
		for step := range steps {
			if step.ID().Present() {
				input.Steps = append(input.Steps, name(step.ID()))
				owners[reporting.Position(reporting.ViewPosition(step.ID().Pos()))] = step
			}
			if parallel, ok := step.Parallel(); ok {
				collect(parallel.Steps())
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
