package checks

import (
	"iter"
	"slices"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/semantics/condition"
	"actionlint.kjanat.dev/v2/internal/semantics/envname"
	"actionlint.kjanat.dev/v2/internal/semantics/identifier"
	"actionlint.kjanat.dev/v2/internal/semantics/jobneeds"
	"actionlint.kjanat.dev/v2/internal/semantics/permission"
	"actionlint.kjanat.dev/v2/internal/workflow"
)

// Finding associates a diagnostic with its owning step.
type Finding struct {
	diagnostic.Diagnostic
	Step declaration.StepView
}

// Declaration is an immutable check selection. Every call returns independent
// results and may run without any preceding or following traversal callbacks.
type Declaration string

func position(p *workflow.Pos) diagnostic.Position {
	if p == nil {
		return diagnostic.Position{}
	}
	return diagnostic.Position{Line: p.Line, Column: p.Col}
}

func findings(ds []diagnostic.Diagnostic) []Finding {
	out := make([]Finding, len(ds))
	for i, d := range ds {
		out[i] = Finding{Diagnostic: d}
	}
	return out
}

func environment(env declaration.EnvView) []Finding {
	if !env.Present() {
		return nil
	}
	var entries []analyzer.Entry
	resolved := env.Mapping()
	for key := range resolved.KeyedEntries() {
		if name, known := key.ScalarString(); known {
			entries = append(entries, analyzer.Entry{Name: name, State: analyzer.KnownKey, Start: key.Origin().Span.Start})
		}
	}
	return findings(envname.Check(entries))
}

func conditional(value declaration.StringView) []Finding {
	if !value.Present() {
		return nil
	}
	return findings(condition.Check(value.Value(), position(viewPosition(value.Pos()))))
}

func permissions(p declaration.PermissionsView) []Finding {
	if !p.Present() {
		return nil
	}
	input := permission.Declaration{}
	if p.All().Present() {
		input.All = &permission.Value{Text: p.All().Value(), Start: position(viewPosition(p.All().Pos()))}
	}
	for _, scope := range p.Scopes() {
		input.Scopes = append(input.Scopes, permission.Scope{Name: permission.Value{Text: scope.Name().Value(), Start: position(viewPosition(scope.Name().Pos()))}, Level: permission.Value{Text: scope.Value().Value(), Start: position(viewPosition(scope.Value().Pos()))}})
	}
	return findings(permission.Check(input))
}

func (check Declaration) Workflow(w declaration.WorkflowView) []Finding {
	switch check {
	case "events":
		return Events(w)
	case "glob":
		return GlobWorkflow(w)

	case "env-var":
		return environment(w.Env())
	case "permissions":
		return permissions(w.Permissions())
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
			item := jobneeds.Job{ID: jobneeds.Name{Value: job.ID().Value(), Start: position(viewPosition(job.ID().Pos()))}}
			for dep := range job.Needs() {
				item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value(), Start: position(viewPosition(dep.Pos()))})
			}
			input = append(input, item)
		}
		return findings(jobneeds.Check(input))
	}
	return nil
}

func (check Declaration) Job(job declaration.JobView) []Finding {
	switch check {
	case "credentials":
		return Credentials(job)
	case "glob":
		return GlobJob(job)
	case "parallel-steps":
		return ParallelSteps(job)

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
	case "id":
		return identifiers(job)
	}
	return nil
}

func (check Declaration) Step(step declaration.StepView) []Finding {
	switch check {
	case "env-var":
		return environment(step.Env())
	case "if-cond":
		return conditional(step.If())
	}
	return nil
}

func identifiers(job declaration.JobView) []Finding {
	name := func(value declaration.StringView) identifier.Name {
		return identifier.Name{Value: value.Value(), Dynamic: value.ContainsExpression(), Start: position(viewPosition(value.Pos()))}
	}
	input := identifier.Job{}
	if job.ID().Present() {
		input.ID = new(name(job.ID()))
	}
	for value := range job.Needs() {
		input.Needs = append(input.Needs, name(value))
	}
	owners := map[diagnostic.Position]declaration.StepView{}
	var collect func(iter.Seq[declaration.StepView])
	collect = func(steps iter.Seq[declaration.StepView]) {
		for step := range steps {
			if step.ID().Present() {
				input.Steps = append(input.Steps, name(step.ID()))
				owners[position(viewPosition(step.ID().Pos()))] = step
			}
			if parallel, ok := step.Parallel(); ok {
				collect(parallel.Steps())
			}
		}
	}
	collect(job.Steps())
	out := findings(identifier.Check(input))
	for i := range out {
		out[i].Step = owners[out[i].Start]
	}
	return out
}
