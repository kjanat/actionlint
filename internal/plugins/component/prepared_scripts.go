package component

import (
	"context"
	"fmt"
	"math"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	invocationbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/invocationcontext"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/preparedscripts"
)

const PreparedScriptsInterface = preparedbind.InterfaceName

type preparedScriptsKey struct{}
type preparationScope struct {
	prepare     analyzer.ScriptPreparer
	occurrences map[analyzer.OccurrenceID]struct{}
}
type preparedScripts struct{}

func (preparedScripts) Prepare(ctx context.Context, id string, dialect preparedbind.OptionString) (preparedbind.ResultOptionPreparedString, error) {
	scope, _ := ctx.Value(preparedScriptsKey{}).(preparationScope)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	occurrence := analyzer.OccurrenceID(id)
	if _, ok := scope.occurrences[occurrence]; !ok {
		return preparedbind.ResultOptionPreparedStringErr{Value: "script occurrence is outside this analysis call"}, nil
	}
	if scope.prepare == nil {
		return preparedbind.ResultOptionPreparedStringErr{Value: "shell preparation is unavailable"}, nil
	}
	var requested *string
	if dialect.IsSome {
		requested = &dialect.Value
	}
	value, err := scope.prepare(ctx, occurrence, requested)
	if err != nil {
		return preparationFailure(err)
	}
	if value == nil {
		return preparedbind.ResultOptionPreparedStringOk{Value: preparedbind.NonePrepared()}, nil
	}
	prepared := preparedbind.Prepared{Text: value.Text, Sanitized: value.Sanitized, Shell: value.Shell, Startup: value.Startup, Excluded: slices.Clone(value.Excluded)}
	if value.Dialect != nil {
		prepared.Dialect = preparedbind.SomeString(*value.Dialect)
	}
	for _, line := range value.Lines {
		switch line.Kind {
		case analyzer.PreparedScriptLine:
			if line.Line < 1 || uint64(line.Line) > math.MaxUint32 {
				return nil, fmt.Errorf("invalid prepared script line %d", line.Line)
			}
			prepared.Lines = append(prepared.Lines, preparedbind.LineScript{Value: uint32(line.Line)})
		case analyzer.PreparedCommentLine:
			if line.Position.Line < 1 || line.Position.Column < 1 || uint64(line.Position.Line) > math.MaxUint32 || uint64(line.Position.Column) > math.MaxUint32 {
				return nil, fmt.Errorf("invalid prepared comment position %v", line.Position)
			}
			prepared.Lines = append(prepared.Lines, preparedbind.LineComment{Value: invocationbind.Position{Line: uint32(line.Position.Line), Column: uint32(line.Position.Column)}})
		case analyzer.PreparedStartupLine:
			prepared.Lines = append(prepared.Lines, preparedbind.LineStartup{})
		default:
			return nil, fmt.Errorf("invalid prepared line kind %q", line.Kind)
		}
	}
	return preparedbind.ResultOptionPreparedStringOk{Value: preparedbind.SomePrepared(prepared)}, nil
}

func preparationFailure(err error) (preparedbind.ResultOptionPreparedString, error) {
	return preparedbind.ResultOptionPreparedStringErr{Value: err.Error()}, nil
}

// Only run scripts with supplied text can request preparation, even when the
// graph describes additional occurrences.
func suppliedOccurrences(input analyzer.Input) map[analyzer.OccurrenceID]struct{} {
	occurrences := make(map[analyzer.OccurrenceID]struct{}, len(input.Scripts)+len(input.Invoked))
	for _, scripts := range [][]analyzer.Script{input.Scripts, input.Invoked} {
		for _, script := range scripts {
			if script.Occurrence.ID != "" && script.Occurrence.Kind == analyzer.RunScript {
				occurrences[script.Occurrence.ID] = struct{}{}
			}
		}
	}
	return occurrences
}
