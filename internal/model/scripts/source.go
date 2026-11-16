package scripts

import (
	"slices"

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
func FromInput(input *workflowlang.ActionArgument) Declaration {
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
	if source != nil {
		// Spans index the decoded value; a value replaced after a parse error
		// has no source mapping.
		if source.Value == declaration.Text {
			declaration.Spans = slices.Clone(source.Spans)
		}
		declaration.Comments = workflowfacts.SourceComments(source.Comments)
	}
	return declaration
}
