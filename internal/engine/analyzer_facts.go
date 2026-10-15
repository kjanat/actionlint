package engine

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/mapping"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func documentAnalyzerInput(path string, content []byte, workflow *workflowlang.Workflow, outline DocumentOutline) analyzer.Input {
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
	addMapping := func(scope string, values *mapping.Inputs[*workflowlang.String]) {
		if values == nil {
			return
		}
		record := analyzer.Mapping{Scope: scope}
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
				item.Start = factPosition(entry.Source.Pos)
			}
			record.Entries = append(record.Entries, item)
		})
		input.Mappings = append(input.Mappings, record)
	}
	addEnv := func(scope string, env *workflowlang.Env) {
		if env != nil {
			addMapping(scope, workflowlang.EnvMappingView(env))
		}
	}
	addEnv("env", workflow.Env)
	var steps func(string, string, []*workflowlang.Step)
	steps = func(jobID, scope string, nodes []*workflowlang.Step) {
		for _, step := range nodes {
			stepID := outlineString(step.ID)
			addEnv(scope+".env", step.Env)
			switch exec := step.Exec.(type) {
			case *workflowlang.ExecRun:
				if exec.Run != nil {
					input.Scripts = append(input.Scripts, analyzer.Script{JobID: jobID, StepID: stepID, Text: exec.Run.Value, Shell: outlineString(exec.Shell), Start: factPosition(exec.Run.Pos)})
				}
			case *workflowlang.ExecAction:
				addMapping(scope+".with", workflowlang.InputMapping(exec))
			case *workflowlang.ExecParallel:
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

func factPosition(pos *workflowlang.Pos) diagnostic.Position {
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

func actionFactsWorkflow(metadata *ActionMetadata) *workflowlang.Workflow {
	parser := workflowlang.NewSourceParser(metadata.src)
	parser.IndexScalarSources(metadata.schemaRoot)
	job := &workflowlang.Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, compositeScriptStep(step, parser))
	}
	return &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"": job}}
}
