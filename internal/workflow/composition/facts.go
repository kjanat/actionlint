package composition

import (
	"actionlint.kjanat.dev/v2/analyzer"
	documentmodel "actionlint.kjanat.dev/v2/document"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
)

func (result Result) AnalyzerInput(path, pathAbs string, content []byte, document documentparse.Document, references map[string]workflowfacts.References) analyzer.Input {
	workflow, outline, units := document.Facts(), document.Outline, result.Batch.Scripts
	refs := references[pathAbs]
	input := analyzer.Input{Available: []analyzer.Fact{analyzer.Source}, Path: path, Content: string(content), Kind: "workflow", ParseStatus: "failed"}
	switch outline := outline.(type) {
	case documentmodel.Workflow:
		input.ParseStatus = outline.ParseStatus
	case documentmodel.Action:
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
	byPath := map[string]workflowfacts.References{refs.Document.Document: refs}
	for filename, refs := range references {
		byPath[referencePath(filename)] = refs
	}
	if workflow == nil {
		return input
	}
	input.Available = append(input.Available, analyzer.Mappings, analyzer.Dependencies)
	if result.CollectScripts {
		input.Available = append(input.Available, analyzer.Scripts, analyzer.Invocations)
		for _, unit := range units {
			script := scriptexecution.Fact(unit, path, pathAbs, result.ResolveExecution)
			if unit.Origin != nil && unit.Origin.StepPosition != nil {
				// Aliased scripts share scalar positions, but retain distinct owners.
				pos := unit.Origin.StepPosition
				script.Reference = byPath[referencePath(unit.Path)].Positions[[2]int{pos.Line, pos.Col}]
			}
			if script.Invocation != nil {
				caller := pathAbs
				if unit.Scope != nil && unit.Scope.Caller != "" {
					caller = unit.Scope.Caller
				}
				script.Invocation.Reference = byPath[referencePath(caller)].Positions[[2]int{script.Invocation.Step.Line, script.Invocation.Step.Column}]
			}

			switch {
			case unit.Input != nil:
				input.ActionInputs = append(input.ActionInputs, analyzer.ActionInput{Script: script, Site: unit.Input.Site})
			case script.Path == "":
				input.Scripts = append(input.Scripts, script)
			default:
				input.Invoked = append(input.Invoked, script)
			}
		}
	}
	if result.ResolveExecution {
		input.Available = append(input.Available, analyzer.Execution)
	}
	input.Mappings, input.Dependencies = workflowfacts.Declarations(content, workflow, refs)
	return input
}
