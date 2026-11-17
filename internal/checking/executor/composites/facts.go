package composites

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// CheckInput projects a composition result onto the check input of its
// entry document. A batch that disagrees with the entry snapshot is rejected.
func CheckInput(result Composition, path, pathAbs string, content []byte, document parsing.Document, references map[string]facts.SourceIndex) (checkinputs.AnalyzerInput, error) {
	units := result.Batch.Scripts
	refs := references[pathAbs]
	input := executor.SourceInput(document, path, content, refs.Document)
	if !document.CanAnalyze() {
		return input, nil
	}
	if !result.Batch.Valid() || !result.EntrySnapshot.Present() {
		return input, scriptanalysis.ErrInvalidBatch
	}
	input.CallGraph = result.Batch.CallGraph
	if result.EntrySnapshot.Reference() != input.CallGraph.Entry().Reference {
		return executor.SourceInput(document, path, content, refs.Document), scriptanalysis.ErrInvalidBatch
	}
	if input.Reference == (sourcetext.Reference{}) {
		input.Reference = input.CallGraph.Entry().Reference
	} else if input.Reference != input.CallGraph.Entry().Reference {
		return executor.SourceInput(document, path, content, refs.Document), scriptanalysis.ErrInvalidBatch
	}
	input.AvailableInputs = append(input.AvailableInputs, rule.Mappings, rule.Dependencies, rule.Invocations)
	if result.Batch.Collected {
		input.AvailableInputs = append(input.AvailableInputs, rule.Scripts)
		for index, unit := range units {
			script, exists := result.Batch.Fact(index, result.ResolveExecution)
			if !exists {
				return executor.SourceInput(document, path, content, refs.Document), scriptanalysis.ErrInvalidBatch
			}

			switch {
			case script.Occurrence.Kind == invocation.ScriptActionInput:
				input.ActionInputs = append(input.ActionInputs, checkinputs.ActionInput{Script: script, Site: unit.Input.Site})
			case script.Occurrence.Owner == input.CallGraph.RootID():
				input.Scripts = append(input.Scripts, script)
			default:
				input.Invoked = append(input.Invoked, script)
			}
		}
	}
	if result.ResolveExecution {
		input.AvailableInputs = append(input.AvailableInputs, rule.Execution)
	}
	input.Mappings, input.Dependencies = result.EntrySnapshot.BoundDocument().Declarations()
	return input, nil
}
