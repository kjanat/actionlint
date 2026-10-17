package checks

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/mapping"
	"actionlint.kjanat.dev/internal/semantics/condition"
	"actionlint.kjanat.dev/internal/semantics/envname"
	"actionlint.kjanat.dev/internal/semantics/identifier"
	"actionlint.kjanat.dev/internal/semantics/jobneeds"
	"actionlint.kjanat.dev/internal/semantics/permission"
	"actionlint.kjanat.dev/internal/workflow"
)

// Finding retains the declaration owner when a check covers an entire job.
// The host may attach source identity and invocation policy without rerunning it.
type Finding struct {
	diagnostic.Diagnostic
	Step *workflow.Step
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
func environment(env *workflow.Env) []Finding {
	if env == nil {
		return nil
	}
	var entries []analyzer.Entry
	workflow.InputResolver.Walk(workflow.EnvMappingView(env), nil, func(entry mapping.Entry[*workflow.String]) {
		if entry.Name != nil {
			entries = append(entries, analyzer.Entry{Name: entry.Name.Value, State: analyzer.KnownKey, Start: position(entry.Name.Pos)})
		}
	})
	return findings(envname.Check(entries))
}
func conditional(value *workflow.String) []Finding {
	if value == nil {
		return nil
	}
	return findings(condition.Check(value.Value, position(value.Pos)))
}
func permissions(p *workflow.Permissions) []Finding {
	if p == nil {
		return nil
	}
	input := permission.Declaration{}
	if p.All != nil {
		input.All = &permission.Value{Text: p.All.Value, Start: position(p.All.Pos)}
	}
	for _, scope := range p.Scopes {
		input.Scopes = append(input.Scopes, permission.Scope{Name: permission.Value{Text: scope.Name.Value, Start: position(scope.Name.Pos)}, Level: permission.Value{Text: scope.Value.Value, Start: position(scope.Value.Pos)}})
	}
	return findings(permission.Check(input))
}

func (check Declaration) Workflow(w *workflow.Workflow) []Finding {
	switch check {
	case "events":
		return Events(w)
	case "glob":
		return GlobWorkflow(w)

	case "env-var":
		return environment(w.Env)
	case "permissions":
		return permissions(w.Permissions)
	case "job-needs":
		jobs := slices.Collect(maps.Values(w.Jobs))
		slices.SortStableFunc(jobs, func(a, b *workflow.Job) int {
			if a.Pos.Line != b.Pos.Line {
				return a.Pos.Line - b.Pos.Line
			}
			return a.Pos.Col - b.Pos.Col
		})
		input := make([]jobneeds.Job, 0, len(jobs))
		for _, job := range jobs {
			item := jobneeds.Job{ID: jobneeds.Name{Value: job.ID.Value, Start: position(job.ID.Pos)}}
			for _, dep := range job.Needs {
				item.Needs = append(item.Needs, jobneeds.Name{Value: dep.Value, Start: position(dep.Pos)})
			}
			input = append(input, item)
		}
		return findings(jobneeds.Check(input))
	}
	return nil
}
func (check Declaration) Job(job *workflow.Job) []Finding {
	switch check {
	case "credentials":
		return Credentials(job)
	case "glob":
		return GlobJob(job)
	case "parallel-steps":
		return ParallelSteps(job)

	case "env-var":
		out := environment(job.Env)
		if job.Container != nil {
			out = append(out, environment(job.Container.Env)...)
		}
		if job.Services != nil {
			for _, service := range job.Services.Value {
				out = append(out, environment(service.Container.Env)...)
			}
		}
		return out
	case "permissions":
		return permissions(job.Permissions)
	case "if-cond":
		out := conditional(job.If)
		if job.Snapshot != nil {
			out = append(out, conditional(job.Snapshot.If)...)
		}
		return out
	case "id":
		return identifiers(job)
	}
	return nil
}
func (check Declaration) Step(step *workflow.Step) []Finding {
	switch check {
	case "env-var":
		return environment(step.Env)
	case "if-cond":
		return conditional(step.If)
	}
	return nil
}
func identifiers(job *workflow.Job) []Finding {
	name := func(value *workflow.String) identifier.Name {
		return identifier.Name{Value: value.Value, Dynamic: value.ContainsExpression(), Start: position(value.Pos)}
	}
	input := identifier.Job{}
	if job.ID != nil {
		input.ID = new(name(job.ID))
	}
	for _, value := range job.Needs {
		input.Needs = append(input.Needs, name(value))
	}
	owners := map[diagnostic.Position]*workflow.Step{}
	var collect func([]*workflow.Step)
	collect = func(steps []*workflow.Step) {
		for _, step := range steps {
			if step.ID != nil {
				input.Steps = append(input.Steps, name(step.ID))
				owners[position(step.ID.Pos)] = step
			}
			if parallel, ok := step.Exec.(*workflow.ExecParallel); ok {
				collect(parallel.Steps)
			}
		}
	}
	collect(job.Steps)
	out := findings(identifier.Check(input))
	for i := range out {
		out[i].Step = owners[out[i].Start]
	}
	return out
}
