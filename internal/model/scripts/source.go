package scripts

import (
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// FromRun extracts declared script text and shell with their original source mapping.
func FromRun(run workflowdecl.ExecRunView) Declaration {
	declaration := fromSource(run.Run(), run.Source())
	declaration.Shell = run.Shell().Value()
	return declaration
}

// FromInput extracts script text supplied as an action input. It declares no shell.
func FromInput(input workflowdecl.InputView) Declaration {
	return fromSource(input.Value(), input.Source())
}

func fromSource(text workflowdecl.StringView, source workflowdecl.ScriptSourceView) Declaration {
	declaration := Declaration{Text: text.Value()}
	if position, present := text.Pos(); present {
		declaration.Start = workflowfacts.Position(&position)
	}
	for span := range source.LineSpans() {
		declaration.Spans = append(declaration.Spans, Span{
			Line: span.Line, Start: span.Start, End: span.End,
			Document:    results.Position{Line: span.YamlLine, Column: span.YamlColumn},
			DocumentEnd: results.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn},
		})
	}
	declaration.Comments = workflowfacts.SourceComments(source.Comments())
	return declaration
}
