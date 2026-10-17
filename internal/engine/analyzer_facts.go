package engine

import (
	"actionlint.kjanat.dev/internal/sourceid"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/internal/workflow/facts"
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
	input.Reference = refs.Document
	referencePath := func(p string) string {
		if p == path || p == "" {
			p = pathAbs
		}
		return sourceid.Document(p)
	}
	byPath := map[string]sourceReferences{refs.Document.Document: refs}
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
				script.Reference = byPath[referencePath(unit.path)].Positions[[2]int{pos.Line, pos.Col}]
			}
			if script.Invocation != nil {
				caller := pathAbs
				if unit.scope != nil && unit.scope.caller != "" {
					caller = unit.scope.caller
				}
				script.Invocation.Reference = byPath[referencePath(caller)].Positions[[2]int{script.Invocation.Step.Line, script.Invocation.Step.Column}]
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
	input.Mappings, input.Dependencies = workflowfacts.Declarations(content, workflow, refs)
	return input
}

func factPosition(pos *workflowlang.Pos) diagnostic.Position { return workflowfacts.Position(pos) }

func actionFactsWorkflow(metadata *ActionMetadata) *workflowlang.Workflow {
	parser := workflowlang.NewSourceParser(metadata.src)
	parser.IndexScalarSources(metadata.schemaRoot)
	job := &workflowlang.Job{}
	for _, step := range metadata.Runs.Steps {
		job.Steps = append(job.Steps, compositeScriptStep(step, parser))
	}
	return &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"": job}}
}
