package workflows

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

type declarationSet struct {
	mappings     []facts.Mapping
	dependencies []facts.Dependency
}

// Declarations accepts a nil BoundDocument, which declares none.
func (b *BoundDocument) Declarations() ([]facts.Mapping, []facts.Dependency) {
	if b == nil || b.workflow == nil {
		return nil, nil
	}
	var set declarationSet
	workflow := b.workflow
	set.env("env", b.references.Document, workflow.Env)
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		set.job(b, workflow.Jobs[id], b.references.Jobs[id])
	}
	return set.mappings, set.dependencies
}

func (d *declarationSet) job(b *BoundDocument, job *definition.Job, ref sourcetext.Reference) {
	scope := ref.Address
	if job.WorkflowCall != nil {
		d.dependency(job.WorkflowCall.Uses, "workflow", ref)
	}
	d.env(scope+".env", ref, job.Env)
	if job.Container != nil {
		d.env(scope+".container.env", ref, job.Container.Env)
	}
	if job.Services != nil {
		for _, name := range slices.Sorted(maps.Keys(job.Services.Value)) {
			d.env(scope+".services."+name+".env", ref, job.Services.Value[name].Container.Env)
		}
	}
	if job.Strategy != nil && job.Strategy.Matrix != nil {
		d.mapping(scope+".strategy.matrix", ref, MatrixMapping(job.Strategy.Matrix))
	}
	for step := range definition.AllSteps(job.Steps) {
		ref := b.references.Steps[step]
		d.env(ref.Address+".env", ref, step.Env)
		if exec, ok := step.Exec.(*definition.ExecAction); ok {
			d.dependency(exec.Uses, "action", ref)
			d.mapping(ref.Address+".with", ref, ResolvedInputs(exec))
		}
	}
}

func (d *declarationSet) env(scope string, owner sourcetext.Reference, env *definition.Env) {
	if env != nil {
		d.mapping(scope, owner, Environment(env))
	}
}

func (d *declarationSet) dependency(value *definition.String, kind string, ref sourcetext.Reference) {
	if value == nil || value.ContainsExpression() {
		return
	}
	start := sourcetext.ZeroIfNil(value.Pos)
	end := start
	if physical := value.SourceScalar.End; physical.Line > 0 && physical.Column > 0 {
		end = physical
	}
	d.dependencies = append(d.dependencies, facts.Dependency{Reference: ref, Uses: value.Value, Kind: kind, Start: start, End: end})
}

func (d *declarationSet) mapping(scope string, owner sourcetext.Reference, values mappings.Mapping) {
	d.mappings = append(d.mappings, facts.Mapping{
		Scope: scope, Reference: owner, Complete: values.Complete(), Entries: slices.Collect(values.Declarations()),
	})
}
