package declaration

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

func (b *Bound) declarations() (mappings []analyzer.Mapping, dependencies []analyzer.Dependency) {
	if b == nil || b.record == nil || b.record.workflow == nil {
		return nil, nil
	}
	addDependency := func(value *workflowlang.String, kind string, ref source.Reference) {
		if value == nil || value.ContainsExpression() {
			return
		}
		start := workflowfacts.Position(value.Pos)
		end := start
		if physical := value.SourceScalar.End; physical.Line > 0 && physical.Col > 0 {
			end = diagnostic.Position{Line: physical.Line, Column: physical.Col}
		}
		dependencies = append(dependencies, analyzer.Dependency{Reference: ref, Uses: value.Value, Kind: kind, Start: start, End: end})
	}
	addMapping := func(scope string, owner source.Reference, values mapping.Mapping) {
		record := analyzer.Mapping{Scope: scope, Reference: owner, Complete: values.Complete()}
		for declaration := range values.Declarations() {
			state := analyzer.UnknownKey
			switch declaration.State {
			case mapping.KnownKey:
				state = analyzer.KnownKey
			case mapping.InvalidKey:
				state = analyzer.InvalidKey
			}
			record.Entries = append(record.Entries, analyzer.Entry{
				Name: declaration.Name, State: state, Source: declaration.Source,
				Evaluated: declaration.Evaluated, Start: declaration.Origin.Span.Start,
			})
		}
		mappings = append(mappings, record)
	}
	addEnv := func(scope string, owner source.Reference, env *workflowlang.Env) {
		if env != nil {
			addMapping(scope, owner, b.record.environments[env])
		}
	}
	addEnv("env", b.references.Document, b.record.workflow.Env)
	var steps func([]*workflowlang.Step)
	steps = func(nodes []*workflowlang.Step) {
		for _, step := range nodes {
			ref := b.references.Steps[step]
			addEnv(ref.Address+".env", ref, step.Env)
			switch exec := step.Exec.(type) {
			case *workflowlang.ExecAction:
				addDependency(exec.Uses, "action", ref)
				addMapping(ref.Address+".with", ref, b.record.resolved[exec])
			case *workflowlang.ExecParallel:
				steps(exec.Steps)
			}
		}
	}
	workflow := b.record.workflow
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[id]
		jobRef := b.references.Jobs[id]
		scope := jobRef.Address
		if job.WorkflowCall != nil {
			addDependency(job.WorkflowCall.Uses, "workflow", jobRef)
		}
		addEnv(scope+".env", jobRef, job.Env)
		if job.Container != nil {
			addEnv(scope+".container.env", jobRef, job.Container.Env)
		}
		if job.Services != nil {
			for _, name := range slices.Sorted(maps.Keys(job.Services.Value)) {
				addEnv(scope+".services."+name+".env", jobRef, job.Services.Value[name].Container.Env)
			}
		}
		if job.Strategy != nil && job.Strategy.Matrix != nil {
			addMapping(scope+".strategy.matrix", jobRef, b.record.matrixMaps[job.Strategy.Matrix])
		}
		steps(job.Steps)
	}
	return mappings, dependencies
}
