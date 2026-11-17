package scripts

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// FromRun extracts declared script text and shell with their original source mapping.
func FromRun(run *workflowlang.ExecRun) Declaration {
	if run == nil {
		return Declaration{}
	}
	declaration := fromSource(run.Run, run.Source)
	declaration.Shell = run.Shell.Text()
	return declaration
}

// FromInput extracts script text supplied as an action input. It declares no shell.
func FromInput(input *workflowlang.Input) Declaration {
	if input == nil {
		return Declaration{}
	}
	return fromSource(input.Value, input.Source)
}

func fromSource(text *workflowlang.String, source *workflowlang.ScriptSource) Declaration {
	declaration := Declaration{Text: text.Text()}
	if position, present := text.Position(); present {
		declaration.Start = sourcepos.ZeroIfNil(&position)
	}
	for _, span := range source.LineSpans() {
		declaration.Spans = append(declaration.Spans, Span{
			Line: span.Line, Start: span.Start, End: span.End,
			Document:    sourcepos.Position{Line: span.YamlLine, Column: span.YamlColumn},
			DocumentEnd: sourcepos.Position{Line: span.YamlEndLine, Column: span.YamlEndColumn},
		})
	}
	if source != nil {
		declaration.Comments = workflowfacts.SourceComments(source.Comments)
	}
	return declaration
}
