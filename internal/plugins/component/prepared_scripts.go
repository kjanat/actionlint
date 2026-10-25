package component

import (
	"context"
	"fmt"
	"math"
	"slices"

	"actionlint.kjanat.dev/v2/analyzer"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/preparedscripts"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/source"
)

const PreparedScriptsInterface = preparedbind.InterfaceName

type preparedScriptsKey struct{}
type preparationScope struct {
	prepare    analyzer.ScriptPreparer
	references map[source.Reference][]analyzer.SourceComment
}
type preparedScripts struct{}

func (preparedScripts) Prepare(ctx context.Context, ref annotationsbind.SourceReference, dialect preparedbind.OptionString) (preparedbind.ResultOptionPreparedString, error) {
	scope, _ := ctx.Value(preparedScriptsKey{}).(preparationScope)
	reference := source.Reference{Document: ref.Document, Revision: ref.Revision, Node: ref.Node, Address: ref.Address}
	if _, ok := scope.references[reference]; !ok {
		return preparedbind.ResultOptionPreparedStringErr{Value: "script reference is outside this analysis call"}, nil
	}
	if scope.prepare == nil {
		return preparedbind.ResultOptionPreparedStringErr{Value: "shell preparation is unavailable"}, nil
	}
	var requested *string
	if dialect.IsSome {
		requested = &dialect.Value
	}
	value, err := scope.prepare(ctx, reference, requested)
	if err != nil {
		return preparedbind.ResultOptionPreparedStringErr{Value: err.Error()}, nil
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
			prepared.Lines = append(prepared.Lines, preparedbind.LineComment{Value: annotationsbind.Position{Line: uint32(line.Position.Line), Column: uint32(line.Position.Column)}})
		case analyzer.PreparedStartupLine:
			prepared.Lines = append(prepared.Lines, preparedbind.LineStartup{})
		default:
			return nil, fmt.Errorf("invalid prepared line kind %q", line.Kind)
		}
	}
	return preparedbind.ResultOptionPreparedStringOk{Value: preparedbind.SomePrepared(prepared)}, nil
}
