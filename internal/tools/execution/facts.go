package execution

import (
	"actionlint.kjanat.dev/analyzer"
	diagmodel "actionlint.kjanat.dev/diagnostic"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/internal/workflow/execution"
	workflowfacts "actionlint.kjanat.dev/internal/workflow/facts"
)

func Fact(unit Unit, document, documentAbs string, includeExecution bool) analyzer.Script {
	run := unit.Run
	script := analyzer.Script{Text: run.Run.Value, Shell: stringValue(run.Shell), Start: workflowfacts.Position(run.Run.Pos)}
	if unit.Origin != nil {
		script.JobID, script.StepID = unit.Origin.Job, unit.Origin.StepID
	}
	if unit.Path != "" && unit.Path != document && unit.Path != documentAbs {
		script.Path = unit.Path
	}
	for _, span := range run.Source.LineSpans() {
		script.Spans = append(script.Spans, analyzer.Span{
			Line: span.Line, Start: span.Start, End: span.End,
			Document:    diagmodel.Position{Line: span.YamlLine, Column: span.YamlColumn},
			DocumentEnd: diagmodel.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn},
		})
	}
	if scope := unit.Scope; scope != nil && script.Path != "" && scope.Call != nil {
		invocation := &analyzer.Invocation{Step: workflowfacts.Position(scope.Call.Pos), Depth: scope.Depth}
		if scope.Caller != document && scope.Caller != documentAbs {
			invocation.Caller = scope.Caller
		}
		if action, ok := scope.Call.Exec.(*workflowlang.ExecAction); ok && action.Uses != nil {
			invocation.Uses = action.Uses.Value
		}
		script.Invocation = invocation
	}
	if includeExecution {
		script.Runtime = &analyzer.Runtime{
			Shell: unit.Execution.Shell.Name, ShellKnown: unit.Execution.Shell.Name != "", ShellImplicit: unit.Execution.Shell.Implicit,
			Directory: unit.Execution.Directory.Path, DirectoryKnown: unit.Execution.Directory.Kind == workflowexecution.DirectoryKnown,
		}
	}
	return script
}

func stringValue(value *workflowlang.String) string {
	if value == nil {
		return ""
	}
	return value.Value
}
