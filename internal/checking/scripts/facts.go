package scripts

import (
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowexecution "actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

// Fact projects a validated script; false means the batch or index is unavailable.
func (batch Batch) Fact(index int, includeExecution bool) (checkinputs.Script, bool) {
	if !batch.Valid() || index < 0 || index >= len(batch.data.scripts) {
		return checkinputs.Script{}, false
	}
	unit := batch.data.scripts[index]
	occurrence, exists := batch.data.projection.Script(unit.Occurrence)
	if !exists || occurrence.Declaration != unit.Reference || (occurrence.Kind == checkinputs.ActionInputScript) != (unit.Input != nil) {
		return checkinputs.Script{}, false
	}
	script := fact(unit, batch.data.graph, includeExecution)
	script.Occurrence = occurrence
	return script, true
}

func fact(unit Unit, graph invocation.Graph, includeExecution bool) checkinputs.Script {
	var script checkinputs.Script
	if input := unit.Input; input != nil {
		script.Declaration = scriptmodel.FromInput(input.Value)
	} else {
		script.Declaration = scriptmodel.FromRun(unit.Run)
	}
	if unit.Origin != nil {
		script.JobID, script.StepID = unit.Origin.Job, unit.Origin.StepID
	}
	if unit.Reference.Document != graph.Entry().Reference.Document {
		script.Path = unit.Path
	}
	script.Reference = unit.Reference
	script.Environment = slices.Clone(unit.Environment)
	if includeExecution && unit.Input == nil {
		script.Runtime = &scriptmodel.Execution{
			Shell: unit.Execution.Shell.Name, ShellKnown: unit.Execution.Shell.Name != "", ShellImplicit: unit.Execution.Shell.Implicit,
			Directory: unit.Execution.Directory.Path, DirectoryKnown: unit.Execution.Directory.Kind == workflowexecution.DirectoryKnown,
		}
	}
	return script
}
