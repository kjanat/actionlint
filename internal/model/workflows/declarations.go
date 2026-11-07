package declaration

import (
	"maps"
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

// declarationSet collects the mappings and dependencies one document declares.
// It holds only the new slices it builds; each method reads the Bound it is
// given.
type declarationSet struct {
	mappings     []workflowfacts.Mapping
	dependencies []workflowfacts.Dependency
}

func (b *Bound) declarations() ([]workflowfacts.Mapping, []workflowfacts.Dependency) {
	if b == nil || b.record == nil || b.record.workflow == nil {
		return nil, nil
	}
	var set declarationSet
	workflow := b.record.workflow
	set.env(b, "env", b.references.Document, workflow.Env)
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		set.job(b, workflow.Jobs[id], b.references.Jobs[id])
	}
	return set.mappings, set.dependencies
}

func (d *declarationSet) job(b *Bound, job *workflowlang.Job, ref sourcepos.Reference) {
	scope := ref.Address
	if job.WorkflowCall != nil {
		d.dependency(job.WorkflowCall.Uses, "workflow", ref)
	}
	d.env(b, scope+".env", ref, job.Env)
	if job.Container != nil {
		d.env(b, scope+".container.env", ref, job.Container.Env)
	}
	if job.Services != nil {
		for _, name := range slices.Sorted(maps.Keys(job.Services.Value)) {
			d.env(b, scope+".services."+name+".env", ref, job.Services.Value[name].Container.Env)
		}
	}
	if job.Strategy != nil && job.Strategy.Matrix != nil {
		d.mapping(scope+".strategy.matrix", ref, b.record.matrixMaps[job.Strategy.Matrix])
	}
	d.steps(b, job.Steps)
}

func (d *declarationSet) steps(b *Bound, nodes []*workflowlang.Step) {
	for _, step := range nodes {
		ref := b.references.Steps[step]
		d.env(b, ref.Address+".env", ref, step.Env)
		switch exec := step.Exec.(type) {
		case *workflowlang.ExecAction:
			d.dependency(exec.Uses, "action", ref)
			d.mapping(ref.Address+".with", ref, b.record.resolved[exec])
		case *workflowlang.ExecParallel:
			d.steps(b, exec.Steps)
		}
	}
}

func (d *declarationSet) env(b *Bound, scope string, owner sourcepos.Reference, env *workflowlang.Env) {
	if env != nil {
		d.mapping(scope, owner, b.record.environments[env])
	}
}

func (d *declarationSet) dependency(value *workflowlang.String, kind string, ref sourcepos.Reference) {
	if value == nil || value.ContainsExpression() {
		return
	}
	start := workflowfacts.Position(value.Pos)
	end := start
	if physical := value.SourceScalar.End; physical.Line > 0 && physical.Col > 0 {
		end = sourcepos.Position{Line: physical.Line, Column: physical.Col}
	}
	d.dependencies = append(d.dependencies, workflowfacts.Dependency{Reference: ref, Uses: value.Value, Kind: kind, Start: start, End: end})
}

func (d *declarationSet) mapping(scope string, owner sourcepos.Reference, values mapping.Mapping) {
	record := workflowfacts.Mapping{Scope: scope, Reference: owner, Complete: values.Complete()}
	for declaration := range values.Declarations() {
		state := workflowfacts.UnknownKey
		switch declaration.State {
		case mapping.KnownKey:
			state = workflowfacts.KnownKey
		case mapping.InvalidKey:
			state = workflowfacts.InvalidKey
		case mapping.UnknownKey:
		}
		record.Entries = append(record.Entries, workflowfacts.Entry{
			Name: declaration.Name, State: state, Source: declaration.Source,
			Evaluated: declaration.Evaluated, Start: declaration.Origin.Span.Start,
		})
	}
	d.mappings = append(d.mappings, record)
}
