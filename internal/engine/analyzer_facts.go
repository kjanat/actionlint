package engine

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/internal/sourceid"
	"actionlint.kjanat.dev/internal/yamlsource"
	"actionlint.kjanat.dev/source"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/mapping"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

type factAvailability struct {
	scripts   bool
	execution bool
}

func registeredRuleNames(registrations []analyzer.Registration) []string {
	var names []string
	for _, registration := range registrations {
		if registration.Analyzer != nil {
			names = append(names, registration.Analyzer.Descriptor().Name)
		}
	}
	return names
}

func requiredFacts(registrations []analyzer.Registration) map[analyzer.Fact]bool {
	facts := map[analyzer.Fact]bool{}
	for _, registration := range registrations {
		if registration.Analyzer == nil {
			continue
		}
		for _, fact := range registration.Analyzer.Descriptor().Requires {
			facts[fact] = true
		}
	}
	return facts
}

func scriptFact(unit scriptUnit, document, documentAbs string, execution bool) analyzer.Script {
	run := unit.run
	script := analyzer.Script{Text: run.Run.Value, Shell: outlineString(run.Shell), Start: factPosition(run.Run.Pos)}
	if unit.origin != nil {
		script.JobID, script.StepID = unit.origin.job, unit.origin.stepID
	}
	if unit.path != "" && unit.path != document && unit.path != documentAbs {
		script.Path = unit.path
	}
	for _, span := range run.Source.LineSpans() {
		script.Spans = append(script.Spans, analyzer.Span{
			Line: span.Line, Start: span.Start, End: span.End,
			Document:    diagnostic.Position{Line: span.YamlLine, Column: span.YamlColumn},
			DocumentEnd: diagnostic.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn},
		})
	}
	if scope := unit.scope; scope != nil && script.Path != "" && scope.call != nil {
		invocation := &analyzer.Invocation{Step: factPosition(scope.call.Pos), Depth: scope.depth}
		if scope.caller != document && scope.caller != documentAbs {
			invocation.Caller = scope.caller
		}
		if action, ok := scope.call.Exec.(*workflowlang.ExecAction); ok && action.Uses != nil {
			invocation.Uses = action.Uses.Value
		}
		script.Invocation = invocation
	}
	if execution {
		script.Runtime = &analyzer.Runtime{
			Shell: unit.execution.shell.name, ShellKnown: unit.execution.shell.name != "", ShellImplicit: unit.execution.shell.implicit,
			Directory: unit.execution.directory.path, DirectoryKnown: unit.execution.directory.kind == directoryKnown,
		}
	}
	return script
}

func documentAnalyzerInput(path, pathAbs string, content []byte, workflow *workflowlang.Workflow, outline DocumentOutline, units []scriptUnit, facts factAvailability, sources map[string][]byte) analyzer.Input {
	refs := workflowReferences(pathAbs, content, workflow, false)
	input := analyzer.Input{Available: []analyzer.Fact{analyzer.Source}, Path: path, Content: string(content), Kind: "workflow", ParseStatus: "failed"}
	switch outline := outline.(type) {
	case WorkflowOutline:
		input.ParseStatus = outline.ParseStatus
	case ActionOutline:
		refs = workflowReferences(pathAbs, content, workflow, true)
		input.Kind = "action"
		input.ParseStatus = outline.ParseStatus
	}
	input.Reference = refs.document
	referencePath := func(p string) string {
		if p == path || p == "" {
			p = pathAbs
		}
		return sourceid.Document(p)
	}
	byPath := map[string]sourceReferences{refs.document.Document: refs}
	// Caller actions may contain only uses steps, so they have no script unit.
	// Index every loaded metadata source before linking invocation identities.
	for filename, content := range sources {
		key := referencePath(filename)
		if _, exists := byPath[key]; exists {
			continue
		}
		parsed := parseDocument(filename, filename, content)
		workflow := parsed.workflow
		if parsed.metadata != nil && workflow != nil {
			workflow = actionFactsWorkflow(parsed.metadata)
		}
		byPath[key] = workflowReferences(filename, content, workflow, parsed.metadata != nil)
	}
	for _, unit := range units {
		if len(unit.content) == 0 {
			continue
		}
		key := referencePath(unit.path)
		if _, exists := byPath[key]; exists {
			continue
		}
		parsed := parseDocument(unit.path, unit.path, unit.content)
		w := parsed.workflow
		if parsed.metadata != nil && w != nil {
			w = actionFactsWorkflow(parsed.metadata)
		}
		byPath[key] = workflowReferences(unit.path, unit.content, w, parsed.metadata != nil)
	}
	if workflow == nil {
		return input
	}
	input.Available = append(input.Available, analyzer.Mappings, analyzer.Dependencies)
	if facts.scripts {
		input.Available = append(input.Available, analyzer.Scripts, analyzer.Invocations)
		for _, unit := range units {
			script := scriptFact(unit, path, pathAbs, facts.execution)
			if unit.origin != nil && unit.origin.stepPosition != nil {
				// Aliased scripts share scalar positions, but retain distinct owners.
				pos := unit.origin.stepPosition
				script.Reference = byPath[referencePath(unit.path)].positions[[2]int{pos.Line, pos.Col}]
			}
			if script.Invocation != nil {
				caller := pathAbs
				if unit.scope != nil && unit.scope.caller != "" {
					caller = unit.scope.caller
				}
				script.Invocation.Reference = byPath[referencePath(caller)].positions[[2]int{script.Invocation.Step.Line, script.Invocation.Step.Column}]
			}

			if script.Path == "" {
				input.Scripts = append(input.Scripts, script)
			} else {
				input.Invoked = append(input.Invoked, script)
			}
		}
	}
	if facts.execution {
		input.Available = append(input.Available, analyzer.Execution)
	}
	addDependency := func(value *workflowlang.String, kind string, ref source.Reference) {
		if value == nil || value.ContainsExpression() {
			return
		}
		start := factPosition(value.Pos)
		end := start
		if scalar, err := yamlsource.NewDocument(string(content)).Decode(yamlsource.Position{Line: start.Line, Column: start.Column}, yamlsource.Context{Indent: start.Column - 2}); err == nil && scalar.Value == value.Value {
			end = diagnostic.Position{Line: scalar.End.Line, Column: scalar.End.Column}
		}
		input.Dependencies = append(input.Dependencies, analyzer.Dependency{Reference: ref, Uses: value.Value, Kind: kind, Start: start, End: end})
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
				item.Start = factPosition(entry.Source.Pos)
			}
			record.Entries = append(record.Entries, item)
		})
		input.Mappings = append(input.Mappings, record)
	}
	addEnv := func(scope string, owner source.Reference, env *workflowlang.Env) {
		if env != nil {
			addMapping(scope, owner, workflowlang.EnvMappingView(env))
		}
	}
	addEnv("env", refs.document, workflow.Env)
	var steps func(string, []*workflowlang.Step)
	steps = func(scope string, nodes []*workflowlang.Step) {
		for _, step := range nodes {
			ref := refs.steps[step]
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
		scope := refs.jobs[id].Address
		if job.WorkflowCall != nil {
			addDependency(job.WorkflowCall.Uses, "workflow", refs.jobs[id])
		}
		addEnv(scope+".env", refs.jobs[id], job.Env)
		if job.Container != nil {
			addEnv(scope+".container.env", refs.jobs[id], job.Container.Env)
		}
		if job.Services != nil {
			for _, name := range slices.Sorted(maps.Keys(job.Services.Value)) {
				addEnv(scope+".services."+name+".env", refs.jobs[id], job.Services.Value[name].Container.Env)
			}
		}
		if job.Strategy != nil && job.Strategy.Matrix != nil {
			addMapping(scope+".strategy.matrix", refs.jobs[id], matrixKeysView(job.Strategy.Matrix))
		}
		steps(scope+".steps", job.Steps)
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
