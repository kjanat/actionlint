package composites

import (
	"actionlint.kjanat.dev/v2/internal/checking/executor"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// CheckInput projects a composition result onto the check input of its
// entry document. A batch that disagrees with the entry snapshot is rejected.
func CheckInput(result Result, path, pathAbs string, content []byte, document parsing.Document, references map[string]workflowfacts.SourceIndex) (checkinputs.Input, error) {
	units := result.Batch.Scripts()
	refs := references[pathAbs]
	input := executor.SourceInput(document, path, content, refs.Document)
	if !document.CanAnalyze() {
		return input, nil
	}
	if !result.Batch.Valid() || !result.EntrySnapshot.Present() {
		return input, scriptexecution.ErrInvalidBatch
	}
	input.CallGraph = result.Batch.CallGraph()
	if result.EntrySnapshot.Reference() != input.CallGraph.Entry().Reference {
		return executor.SourceInput(document, path, content, refs.Document), scriptexecution.ErrInvalidBatch
	}
	if input.Reference == (source.Reference{}) {
		input.Reference = input.CallGraph.Entry().Reference
	} else if input.Reference != input.CallGraph.Entry().Reference {
		return executor.SourceInput(document, path, content, refs.Document), scriptexecution.ErrInvalidBatch
	}
	input.AvailableInputs = append(input.AvailableInputs, rulemeta.Mappings, rulemeta.Dependencies, rulemeta.Invocations)
	if result.Batch.CollectedScripts() {
		input.AvailableInputs = append(input.AvailableInputs, rulemeta.Scripts)
		for index, unit := range units {
			script, exists := result.Batch.Fact(index, result.ResolveExecution)
			if !exists {
				return executor.SourceInput(document, path, content, refs.Document), scriptexecution.ErrInvalidBatch
			}

			switch {
			case script.Occurrence.Kind == checkinputs.ActionInputScript:
				input.ActionInputs = append(input.ActionInputs, checkinputs.ActionInput{Script: script, Site: unit.Input.Site})
			case script.Occurrence.Owner == input.CallGraph.Entry().ID:
				input.Scripts = append(input.Scripts, script)
			default:
				input.Invoked = append(input.Invoked, script)
			}
		}
	}
	if result.ResolveExecution {
		input.AvailableInputs = append(input.AvailableInputs, rulemeta.Execution)
	}
	input.Mappings, input.Dependencies = result.EntrySnapshot.Declarations()
	return input, nil
}
