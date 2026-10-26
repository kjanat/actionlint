package composition

import (
	"actionlint.kjanat.dev/v2/analyzer"
	documentmodel "actionlint.kjanat.dev/v2/document"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
)

func (result Result) AnalyzerInput(path, pathAbs string, content []byte, document documentparse.Document, references map[string]workflowfacts.References) (analyzer.Input, error) {
	workflow, outline, units := document.Facts(), document.Outline, result.Batch.Scripts()
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
	if workflow == nil {
		return input, nil
	}
	if !result.Batch.Valid() {
		return input, scriptexecution.ErrInvalidBatch
	}
	input.Available = append(input.Available, analyzer.Mappings, analyzer.Dependencies)
	if result.Batch.CollectedScripts() {
		input.Available = append(input.Available, analyzer.Scripts, analyzer.Invocations)
		for index, unit := range units {
			script, _ := result.Batch.Fact(index, result.ResolveExecution)

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
	return input, nil
}
