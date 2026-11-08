package component

import (
	"context"
	"fmt"
	"math"
	"slices"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	shellprofile "actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/shell"
	shellinputbind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-script/shellscriptinputs"
)

const ShellScriptInputsInterface = shellinputbind.InterfaceName

type (
	shellScriptInputsKey  struct{}
	shellScriptInputScope struct {
		prepare     checkinputs.ShellScriptInputBuilder
		occurrences map[checkinputs.OccurrenceID]struct{}
	}
)
type shellScriptInputs struct{}

func newShellScriptInputScope(input checkinputs.Input) shellScriptInputScope {
	prepare := input.BuildShellScriptInput
	if prepare == nil {
		prepare = shellprofile.NewShellScriptInputBuilder(slices.Concat(input.Scripts, input.Invoked))
	}
	return shellScriptInputScope{prepare: prepare, occurrences: suppliedOccurrences(input)}
}

func (shellScriptInputs) ForScript(ctx context.Context, id string, dialect shellinputbind.OptionString) (shellinputbind.ResultOptionInputString, error) {
	scope, _ := ctx.Value(shellScriptInputsKey{}).(shellScriptInputScope)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	occurrence := checkinputs.OccurrenceID(id)
	if _, ok := scope.occurrences[occurrence]; !ok {
		return shellinputbind.ResultOptionInputStringErr{Value: "script occurrence is outside this analysis call"}, nil
	}
	if scope.prepare == nil {
		return shellinputbind.ResultOptionInputStringErr{Value: "shell preparation is unavailable"}, nil
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
		return shellinputbind.ResultOptionInputStringOk{Value: shellinputbind.NoneInput()}, nil
	}
	guestInput := shellinputbind.Input{Text: value.Text, MaskedText: value.MaskedText, Shell: value.Shell, ShellSetup: value.ShellSetup, Excluded: slices.Clone(value.Excluded)}
	if value.Dialect != nil {
		guestInput.Dialect = shellinputbind.SomeString(*value.Dialect)
	}
	for _, line := range value.Lines {
		switch line.Kind {
		case checkinputs.ShellScriptSourceLine:
			if line.Line < 1 || uint64(line.Line) > math.MaxUint32 {
				return nil, fmt.Errorf("invalid source script line %d", line.Line)
			}
			guestInput.Lines = append(guestInput.Lines, shellinputbind.LineSource{Value: uint32(line.Line)})
		case checkinputs.ShellScriptCommentLine:
			if line.Position.Line < 1 || line.Position.Column < 1 || uint64(line.Position.Line) > math.MaxUint32 || uint64(line.Position.Column) > math.MaxUint32 {
				return nil, fmt.Errorf("invalid source comment position %v", line.Position)
			}
			guestInput.Lines = append(guestInput.Lines, shellinputbind.LineComment{Value: shellinputbind.Position{Line: uint32(line.Position.Line), Column: uint32(line.Position.Column)}})
		case checkinputs.ShellScriptGeneratedLine:
			guestInput.Lines = append(guestInput.Lines, shellinputbind.LineGenerated{})
		default:
			return nil, fmt.Errorf("invalid shell input line kind %q", line.Kind)
		}
	}
	return shellinputbind.ResultOptionInputStringOk{Value: shellinputbind.SomeInput(guestInput)}, nil
}

func shellInputFailure(err error) (shellinputbind.ResultOptionInputString, error) {
	return shellinputbind.ResultOptionInputStringErr{Value: err.Error()}, nil
}

// Only run scripts with supplied text can request preparation, even when the
// graph describes additional occurrences.
func suppliedOccurrences(input checkinputs.Input) map[checkinputs.OccurrenceID]struct{} {
	occurrences := make(map[checkinputs.OccurrenceID]struct{}, len(input.Scripts)+len(input.Invoked))
	for _, scripts := range [][]checkinputs.Script{input.Scripts, input.Invoked} {
		for _, script := range scripts {
			if script.Occurrence.ID != "" && script.Occurrence.Kind == checkinputs.RunScript {
				occurrences[script.Occurrence.ID] = struct{}{}
			}
		}
	}
	return occurrences
}
