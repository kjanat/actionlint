package engine

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/mapping"
)

func documentAnalyzerInput(path string, content []byte, workflow *Workflow, outline DocumentOutline) analyzer.Input {
	input := analyzer.Input{Available: []analyzer.Fact{analyzer.Source}, Path: path, Content: string(content), Kind: "workflow", ParseStatus: "failed"}
	switch outline := outline.(type) {
	case WorkflowOutline:
		input.ParseStatus = outline.ParseStatus
	case ActionOutline:
		input.Kind = "action"
		input.ParseStatus = outline.ParseStatus
	}
	if workflow == nil {
		return input
	}
	input.Available = append(input.Available, analyzer.Scripts, analyzer.Mappings)
	addMapping := func(scope string, values *mapping.Inputs[*String]) {
		if values == nil {
			return
		}
		record := analyzer.Mapping{Scope: scope}
		record.Complete = inputResolver.Walk(values, nil, func(entry mapping.Entry[*String]) {
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
				item.Start = factPosition(entry.Source.Pos)
			}
			record.Entries = append(record.Entries, item)
		})
		input.Mappings = append(input.Mappings, record)
	}
	addEnv := func(scope string, env *Env) {
		if env != nil {
			addMapping(scope, envMappingView(env))
		}
	}
	addEnv("env", workflow.Env)
	var steps func(string, string, []*Step)
	steps = func(jobID, scope string, nodes []*Step) {
		for _, step := range nodes {
			stepID := outlineString(step.ID)
			addEnv(scope+".env", step.Env)
			switch exec := step.Exec.(type) {
			case *ExecRun:
				if exec.Run != nil {
					input.Scripts = append(input.Scripts, analyzer.Script{JobID: jobID, StepID: stepID, Text: exec.Run.Value, Shell: outlineString(exec.Shell), Start: factPosition(exec.Run.Pos)})
				}
			case *ExecAction:
				addMapping(scope+".with", actionInputMapping(exec))
			case *ExecParallel:
				steps(jobID, scope+".parallel", exec.Steps)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(workflow.Jobs)) {
		job := workflow.Jobs[id]
		scope := "jobs." + id
		addEnv(scope+".env", job.Env)
		if job.Container != nil {
			addEnv(scope+".container.env", job.Container.Env)
		}
		if job.Services != nil {
			for _, name := range slices.Sorted(maps.Keys(job.Services.Value)) {
				addEnv(scope+".services."+name+".env", job.Services.Value[name].Container.Env)
			}
		}
		if job.Strategy != nil && job.Strategy.Matrix != nil {
			addMapping(scope+".strategy.matrix", matrixKeysView(job.Strategy.Matrix))
		}
		steps(id, scope+".steps", job.Steps)
	}
	return input
}

func factPosition(pos *Pos) diagnostic.Position {
	if pos == nil {
		return diagnostic.Position{}
	}
	return diagnostic.Position{Line: pos.Line, Column: pos.Col}
}

func matrixKeysView(matrix *Matrix) *mapping.Inputs[*String] {
	result := &mapping.Inputs[*String]{Expression: matrix.Expression, Values: map[string]mapping.Input[*String]{}}
	for name, row := range matrix.Rows {
		result.Values[name] = mapping.Input[*String]{Name: row.Name, Evaluated: row.evaluated}
	}
	for name, filters := range map[string]*MatrixCombinations{"include": matrix.Include, "exclude": matrix.Exclude} {
		if filters != nil {
			result.Values[name] = mapping.Input[*String]{Name: filters.Name, Evaluated: filters.evaluated}
		}
	}
	for _, insertion := range matrix.Insertions {
		result.Insertions = append(result.Insertions, matrixKeysView(insertion))
	}
	return result
}

func actionFactsWorkflow(metadata *ActionMetadata) *Workflow {
	parser := newSourceParser(metadata.src)
	parser.indexScalarSources(metadata.schemaRoot)
	job := &Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, compositeScriptStep(step, parser))
	}
	return &Workflow{Jobs: map[string]*Job{"": job}}
}
