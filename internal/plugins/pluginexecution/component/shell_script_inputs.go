package component

import (
	"context"
	"fmt"
	"math"
	"slices"

	"actionlint.kjanat.dev/v2/internal/externallinters/preparation/shell"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/shellscriptinputs"
)

const ShellScriptInputsInterface = shellscriptinputs.InterfaceName

type (
	shellScriptInputsKey  struct{}
	shellScriptInputScope struct {
		prepare     checkinputs.ShellScriptInputBuilder
		occurrences map[invocation.ID]struct{}
	}
)
type shellScriptInputs struct{}

func newShellScriptInputScope(input checkinputs.AnalyzerInput) shellScriptInputScope {
	prepare := input.BuildShellScriptInput
	if prepare == nil {
		prepare = shell.NewShellScriptInputBuilder(slices.Concat(input.Scripts, input.Invoked))
	}
	return shellScriptInputScope{prepare: prepare, occurrences: suppliedOccurrences(input)}
}

func (shellScriptInputs) ForScript(ctx context.Context, id string, dialect shellscriptinputs.OptionString) (shellscriptinputs.ResultOptionInputString, error) {
	scope, _ := ctx.Value(shellScriptInputsKey{}).(shellScriptInputScope)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	occurrence := invocation.ID(id)
	if _, ok := scope.occurrences[occurrence]; !ok {
		return shellscriptinputs.ResultOptionInputStringErr{Value: "script occurrence is outside this analysis call"}, nil
	}
	if scope.prepare == nil {
		return shellscriptinputs.ResultOptionInputStringErr{Value: "shell preparation is unavailable"}, nil
	}
	var requested *string
	if dialect.IsSome {
		requested = &dialect.Value
	}
	value, err := scope.prepare(ctx, occurrence, requested)
	if err != nil {
		return shellInputFailure(err)
	}
	if value == nil {
		return shellscriptinputs.ResultOptionInputStringOk{Value: shellscriptinputs.NoneInput()}, nil
	}
	guestInput := shellscriptinputs.Input{Text: value.Text, MaskedText: value.MaskedText, Shell: value.Shell, ShellSetup: value.ShellSetup, Excluded: slices.Clone(value.Excluded)}
	if value.Dialect != nil {
		guestInput.Dialect = shellscriptinputs.SomeString(*value.Dialect)
	}
	for _, line := range value.Lines {
		switch line.Kind {
		case checkinputs.ShellScriptSourceLine:
			if line.Line < 1 || uint64(line.Line) > math.MaxUint32 {
				return nil, fmt.Errorf("invalid source script line %d", line.Line)
			}
			guestInput.Lines = append(guestInput.Lines, shellscriptinputs.LineSource{Value: uint32(line.Line)})
		case checkinputs.ShellScriptCommentLine:
			if line.Position.Line < 1 || line.Position.Column < 1 || uint64(line.Position.Line) > math.MaxUint32 || uint64(line.Position.Column) > math.MaxUint32 {
				return nil, fmt.Errorf("invalid source comment position %v", line.Position)
			}
			guestInput.Lines = append(guestInput.Lines, shellscriptinputs.LineComment{Value: shellscriptinputs.Position{Line: uint32(line.Position.Line), Column: uint32(line.Position.Column)}})
		case checkinputs.ShellScriptGeneratedLine:
			guestInput.Lines = append(guestInput.Lines, shellscriptinputs.LineGenerated{})
		default:
			return nil, fmt.Errorf("invalid shell input line kind %q", line.Kind)
		}
	}
	return shellscriptinputs.ResultOptionInputStringOk{Value: shellscriptinputs.SomeInput(guestInput)}, nil
}

func shellInputFailure(err error) (shellscriptinputs.ResultOptionInputString, error) {
	return shellscriptinputs.ResultOptionInputStringErr{Value: err.Error()}, nil
}

// Only run scripts with supplied text can request preparation, even when the
// graph describes additional occurrences.
func suppliedOccurrences(input checkinputs.AnalyzerInput) map[invocation.ID]struct{} {
	occurrences := make(map[invocation.ID]struct{}, len(input.Scripts)+len(input.Invoked))
	for _, scripts := range [][]checkinputs.Script{input.Scripts, input.Invoked} {
		for _, script := range scripts {
			if script.Occurrence.ID != "" && script.Occurrence.Kind == invocation.ScriptRun {
				occurrences[script.Occurrence.ID] = struct{}{}
			}
		}
	}
	return occurrences
}
