package declarations

import (
	"iter"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	envname "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/env-var"

	identifier "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/id"

	condition "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/if-cond"

	jobneeds "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/job-needs"

	permission "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/permissions"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Declaration is an immutable check selection. Every call returns independent
// results and may run without any preceding or following traversal callbacks.
type Declaration string

func environment(env declaration.EnvView) []reporting.Finding {
	if !env.Present() {
		return nil
	}
	var entries []workflowfacts.Entry
	resolved := env.Mapping()
	for key := range resolved.KeyedEntries() {
		if name, known := key.ScalarString(); known {
			entries = append(entries, workflowfacts.Entry{Name: name, State: workflowfacts.KnownKey, Start: key.Origin().Span.Start})
		}
	}
	return reporting.FromDiagnostics(envname.Check(entries))
}

func conditional(value declaration.StringView) []reporting.Finding {
	if !value.Present() {
		return nil
	}
	return reporting.FromDiagnostics(condition.Check(value.Value(), reporting.Position(reporting.ViewPosition(value.Pos()))))
}

func permissions(p declaration.PermissionsView) []reporting.Finding {
	if !p.Present() {
		return nil
	}
	input := permission.Declaration{}
	if p.All().Present() {
		input.All = &permission.Value{Text: p.All().Value(), Start: reporting.Position(reporting.ViewPosition(p.All().Pos()))}
	}
	for _, scope := range p.Scopes() {
		input.Scopes = append(input.Scopes, permission.Scope{Name: permission.Value{Text: scope.Name().Value(), Start: reporting.Position(reporting.ViewPosition(scope.Name().Pos()))}, Level: permission.Value{Text: scope.Value().Value(), Start: reporting.Position(reporting.ViewPosition(scope.Value().Pos()))}})
	}
	return reporting.FromDiagnostics(permission.Check(input))
}

func (check Declaration) Workflow(w declaration.WorkflowView) []reporting.Finding {
	switch check {
	case "events":
		return correctness.Events(w)
	case "glob":
		return correctness.GlobWorkflow(w)
	case "empty-triggers":
		return suspicious.EmptyTriggers(w)

	case "env-var":
		return environment(w.Env())
	case "permissions":
		return permissions(w.Permissions())
	case "missing-workflow-permissions":
		return security.MissingWorkflowPermissions(w)
	case "job-needs":
		jobs := slices.Collect(func(yield func(declaration.JobView) bool) {
			for _, job := range w.Jobs() {
				if !yield(job) {
					return
				}
			}
		})
		slices.SortStableFunc(jobs, func(a, b declaration.JobView) int {
			left, _ := a.Pos()
			right, _ := b.Pos()
			if left.Line != right.Line {
				return left.Line - right.Line
			}
			return left.Col - right.Col
		})
		input := make([]jobneeds.Job, 0, len(jobs))
		for _, job := range jobs {
			item := jobneeds.Job{ID: jobneeds.Name{Value: job.ID().Value(), Start: reporting.Position(reporting.ViewPosition(job.ID().Pos()))}}
			for dep := range job.Needs() {
				item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value(), Start: reporting.Position(reporting.ViewPosition(dep.Pos()))})
			}
			input = append(input, item)
		}
		return reporting.FromDiagnostics(jobneeds.Check(input))
	}
	return nil
}

func (check Declaration) Job(job declaration.JobView) []reporting.Finding {
	switch check {
	case "credentials":
		return security.Credentials(job)
	case "glob":
		return correctness.GlobJob(job)
	case "parallel-steps":
		return correctness.ParallelSteps(job)
	case "empty-job":
		return suspicious.EmptyJob(job)
	case "numeric-values":
		return correctness.NumericValues(job)

	case "env-var":
		out := environment(job.Env())
		if job.Container().Present() {
			out = append(out, environment(job.Container().Env())...)
		}
		if job.Services().Present() {
			for _, service := range job.Services().Value() {
				out = append(out, environment(service.Container().Env())...)
			}
		}
		return out
	case "permissions":
		return permissions(job.Permissions())
	case "if-cond":
		out := conditional(job.If())
		if job.Snapshot().Present() {
			out = append(out, conditional(job.Snapshot().If())...)
		}
		return out
	case "uncancellable-condition":
		return suspicious.UncancellableCondition(job.If())
	case "id":
		return identifiers(job)
	}
	return nil
}

func (check Declaration) Step(step declaration.StepView) []reporting.Finding {
	switch check {
	case "env-var":
		return environment(step.Env())
	case "if-cond":
		return conditional(step.If())
	case "empty-run":
		return suspicious.EmptyRun(step)
	}
	return nil
}

func identifiers(job declaration.JobView) []reporting.Finding {
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
	owners := map[results.Position]declaration.StepView{}
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
