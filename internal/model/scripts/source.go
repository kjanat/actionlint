package scripts

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func FromRun(run *definition.ExecRun) Declaration {
	if run == nil {
		return Declaration{}
	}
	declaration := fromSource(run.Run, run.Source)
	declaration.Shell = run.Shell.Text()
	return declaration
}

func FromInput(input *definition.ActionArgument) Declaration {
	if input == nil {
		return Declaration{}
	}
	return fromSource(input.Value, input.Source)
}

func fromSource(text *definition.String, source *definition.ScriptSource) Declaration {
	declaration := Declaration{Text: text.Text()}
	if position, present := text.Position(); present {
		declaration.Start = sourcetext.ZeroIfNil(&position)
	}
	if source != nil {
		// Spans index the decoded value; a value replaced after a parse error
		// has no source mapping.
		if source.Value == declaration.Text {
			declaration.Spans = slices.Clone(source.Spans)
		}
		declaration.Comments = facts.SourceComments(source.Comments)
	}
	return declaration
}
