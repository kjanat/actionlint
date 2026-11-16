package scriptanalysis

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
)

// Fact projects a validated script; false means the batch or index is unavailable.
// With includeExecution, a run script always has a Runtime; it is empty when
// the analysis did not resolve execution.
func (batch Batch) Fact(index int, includeExecution bool) (checkinputs.Script, bool) {
	if !batch.Valid() || index < 0 || index >= len(batch.Scripts) {
		return checkinputs.Script{}, false
	}
	unit := batch.Scripts[index]
	occurrence, exists := batch.CallGraph.Script(unit.Occurrence)
	if !exists || occurrence.Declaration != unit.Reference || (occurrence.Kind == invocation.ScriptActionInput) != (unit.Input != nil) {
		return checkinputs.Script{}, false
	}
	script := fact(unit, batch.CallGraph, includeExecution)
	script.Occurrence = occurrence
	return script, true
}

func fact(unit Unit, graph invocation.Graph, includeExecution bool) checkinputs.Script {
	var script checkinputs.Script
	if input := unit.Input; input != nil {
		script.Declaration = scripts.FromInput(input.Value)
	} else {
		script.Declaration = scripts.FromRun(unit.Run)
	}
	if unit.Origin != nil {
		script.JobID, script.StepID = unit.Origin.Job, unit.Origin.StepID
	}
	if unit.Reference.Document != graph.Entry().Reference.Document {
		script.Path = unit.Path
	}
	script.Reference = unit.Reference
	script.Environment = slices.Clone(unit.Environment)
	script.Interpreters = unit.Interpreters
	if includeExecution && unit.Input == nil {
		var runtime scripts.Execution
		if unit.Execution != nil {
			runtime = *unit.Execution
		}
		script.Runtime = &runtime
	}
	return script
}
