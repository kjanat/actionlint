package composition

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

func (result Result) AnalyzerInput(path, pathAbs string, content []byte, document documentparse.Document, references map[string]workflowfacts.SourceIndex) (analyzer.Input, error) {
	units := result.Batch.Scripts()
	refs := references[pathAbs]
	input := document.AnalyzerSource(path, content, refs.Document)
	if !document.CanAnalyze() {
		return input, nil
	}
	if !result.Batch.Valid() || result.EntrySnapshot == nil {
		return input, scriptexecution.ErrInvalidBatch
	}
	input.CallGraph = result.Batch.CallGraph()
	if result.EntrySnapshot.Reference() != input.CallGraph.Entry().Reference {
		return document.AnalyzerSource(path, content, refs.Document), scriptexecution.ErrInvalidBatch
	}
	if input.Reference == (source.Reference{}) {
		input.Reference = input.CallGraph.Entry().Reference
	} else if input.Reference != input.CallGraph.Entry().Reference {
		return document.AnalyzerSource(path, content, refs.Document), scriptexecution.ErrInvalidBatch
	}
	input.Available = append(input.Available, analyzer.Mappings, analyzer.Dependencies, analyzer.Invocations)
	if result.Batch.CollectedScripts() {
		input.Available = append(input.Available, analyzer.Scripts)
		for index, unit := range units {
			script, exists := result.Batch.Fact(index, result.ResolveExecution)
			if !exists {
				return document.AnalyzerSource(path, content, refs.Document), scriptexecution.ErrInvalidBatch
			}

			switch {
			case script.Occurrence.Kind == analyzer.ActionInputScript:
				input.ActionInputs = append(input.ActionInputs, analyzer.ActionInput{Script: script, Site: unit.Input.Site})
			case script.Occurrence.Owner == input.CallGraph.Entry().ID:
				input.Scripts = append(input.Scripts, script)
			default:
				input.Invoked = append(input.Invoked, script)
			}
		}
	}
	if result.ResolveExecution {
		input.Available = append(input.Available, analyzer.Execution)
	}
	input.Mappings, input.Dependencies = result.EntrySnapshot.Declarations()
	return input, nil
}
