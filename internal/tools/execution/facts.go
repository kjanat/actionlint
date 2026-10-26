package execution

import (
	"actionlint.kjanat.dev/v2/analyzer"
	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func Fact(unit Unit, graph invocation.Graph, includeExecution bool) analyzer.Script {
	run := unit.Run
	var script analyzer.Script
	var source *workflowlang.ScriptSource
	if input := unit.Input; input != nil {
		script = analyzer.Script{Text: input.Value.Value.Value, Start: workflowfacts.Position(input.Value.Value.Pos)}
		source = input.Value.Source
	} else {
		script = analyzer.Script{Text: run.Run.Value, Shell: stringValue(run.Shell), Start: workflowfacts.Position(run.Run.Pos)}
		source = run.Source
	}
	if unit.Origin != nil {
		script.JobID, script.StepID = unit.Origin.Job, unit.Origin.StepID
	}
	if unit.Reference.Document != graph.Entry().Reference.Document {
		script.Path = unit.Path
	}
	for _, span := range source.LineSpans() {
		script.Spans = append(script.Spans, analyzer.Span{
			Line: span.Line, Start: span.Start, End: span.End,
			Document:    diagmodel.Position{Line: span.YamlLine, Column: span.YamlColumn},
			DocumentEnd: diagmodel.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn},
		})
	}
	script.Comments = workflowfacts.SourceComments(source)
	script.Reference = unit.Reference
	if scope := unit.Scope; scope != nil {
		if call, exists := graph.Call(scope.Occurrence); exists {
			effects, _ := scope.Context.Effects()
			projected := &analyzer.Invocation{Reference: call.Site, Step: effects.Step, Depth: call.Depth, Uses: call.Uses}
			if call.Site.Document != graph.Entry().Reference.Document {
				projected.Caller = call.Site.Document
			}
			script.Invocation = projected
		}
	}
	if includeExecution && unit.Input == nil {
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
