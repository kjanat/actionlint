package facts

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yamlsource"
	"actionlint.kjanat.dev/v2/source"
)

// Declarations prepares reusable mapping and dependency facts without execution.
func Declarations(content []byte, workflow *workflowlang.Workflow, refs References) (mappings []analyzer.Mapping, dependencies []analyzer.Dependency) {
	if workflow == nil {
		return nil, nil
	}
	addDependency := func(value *workflowlang.String, kind string, ref source.Reference) {
		if value == nil || value.ContainsExpression() {
			return
		}
		start := Position(value.Pos)
		end := start
		if scalar, err := yamlsource.NewDocument(string(content)).Decode(yamlsource.Position{Line: start.Line, Column: start.Column}, yamlsource.Context{Indent: start.Column - 2}); err == nil && scalar.Value == value.Value {
			end = diagnostic.Position{Line: scalar.End.Line, Column: scalar.End.Column}
		}
		dependencies = append(dependencies, analyzer.Dependency{Reference: ref, Uses: value.Value, Kind: kind, Start: start, End: end})
	}
	addMapping := func(scope string, owner source.Reference, values *mapping.Inputs[*workflowlang.String]) {
		if values == nil {
			return
		}
		record := analyzer.Mapping{Scope: scope, Reference: owner}
		record.Complete = workflowlang.InputResolver.Walk(values, nil, func(entry mapping.Entry[*workflowlang.String]) {
			item := analyzer.Entry{State: analyzer.UnknownKey, Evaluated: entry.Evaluated}
			switch entry.State {
			case mapping.KnownKey:
				item.State = analyzer.KnownKey
			case mapping.InvalidKey:
				item.State = analyzer.InvalidKey
			}
			if entry.Name != nil {
				item.Name = entry.Name.Value
			}
			if entry.Source != nil {
				item.Source = entry.Source.Value
				item.Start = Position(entry.Source.Pos)
			}
			record.Entries = append(record.Entries, item)
		})
		mappings = append(mappings, record)
	}
	addEnv := func(scope string, owner source.Reference, env *workflowlang.Env) {
		if env != nil {
			addMapping(scope, owner, workflowlang.EnvMappingView(env))
		}
	}
	addEnv("env", refs.Document, workflow.Env)
	var steps func(string, []*workflowlang.Step)
	steps = func(scope string, nodes []*workflowlang.Step) {
		for _, step := range nodes {
			ref := refs.Steps[step]
			addEnv(ref.Address+".env", ref, step.Env)
			switch exec := step.Exec.(type) {
			case *workflowlang.ExecAction:
				addDependency(exec.Uses, "action", ref)
				addMapping(ref.Address+".with", ref, workflowlang.InputMapping(exec))
			case *workflowlang.ExecParallel:
				steps(scope+".parallel", exec.Steps)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[id]
		scope := refs.Jobs[id].Address
		if job.WorkflowCall != nil {
			addDependency(job.WorkflowCall.Uses, "workflow", refs.Jobs[id])
		}
		addEnv(scope+".env", refs.Jobs[id], job.Env)
		if job.Container != nil {
			addEnv(scope+".container.env", refs.Jobs[id], job.Container.Env)
		}
		if job.Services != nil {
			for _, name := range slices.Sorted(maps.Keys(job.Services.Value)) {
				addEnv(scope+".services."+name+".env", refs.Jobs[id], job.Services.Value[name].Container.Env)
			}
		}
		if job.Strategy != nil && job.Strategy.Matrix != nil {
			addMapping(scope+".strategy.matrix", refs.Jobs[id], matrixKeysView(job.Strategy.Matrix))
		}
		steps(scope+".Steps", job.Steps)
	}
	return mappings, dependencies
}

func Position(pos *workflowlang.Pos) diagnostic.Position {
	if pos == nil {
		return diagnostic.Position{}
	}
	return diagnostic.Position{Line: pos.Line, Column: pos.Col}
}

func matrixKeysView(matrix *workflowlang.Matrix) *mapping.Inputs[*workflowlang.String] {
	result := &mapping.Inputs[*workflowlang.String]{Expression: matrix.Expression, Values: map[string]mapping.Input[*workflowlang.String]{}}
	for name, row := range matrix.Rows {
		result.Values[name] = mapping.Input[*workflowlang.String]{Name: row.Name, Evaluated: row.Evaluated}
	}
	for name, filters := range map[string]*workflowlang.MatrixCombinations{"include": matrix.Include, "exclude": matrix.Exclude} {
		if filters != nil {
			result.Values[name] = mapping.Input[*workflowlang.String]{Name: filters.Name, Evaluated: filters.Evaluated}
		}
	}
	for _, insertion := range matrix.Insertions {
		result.Insertions = append(result.Insertions, matrixKeysView(insertion))
	}
	return result
}
