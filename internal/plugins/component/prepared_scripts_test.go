package component

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	preparedbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/preparedscripts"
	annotationsbind "actionlint.kjanat.dev/v2/internal/plugins/component/bindings/kjanat/actionlint/sourceannotations"
	"actionlint.kjanat.dev/v2/source"
)

func TestPreparedScriptsRejectForeignReference(t *testing.T) {
	called := false
	scope := preparationScope{prepare: func(context.Context, source.Reference, *string) (*analyzer.PreparedScript, error) {
		called = true
		return nil, nil
	}, references: map[source.Reference][]analyzer.SourceComment{{Document: "known"}: nil}}
	ctx := context.WithValue(t.Context(), preparedScriptsKey{}, scope)
	result, err := (preparedScripts{}).Prepare(ctx, annotationsbind.SourceReference{Document: "foreign"}, preparedbind.NoneString())
	if _, ok := result.(preparedbind.ResultOptionPreparedStringErr); !ok || err != nil || called {
		t.Fatalf("foreign reference reached callback: %v, %v", result, err)
	}
	result, err = (preparedScripts{}).Prepare(ctx, annotationsbind.SourceReference{Document: "known"}, preparedbind.NoneString())
	accepted, ok := result.(preparedbind.ResultOptionPreparedStringOk)
	if !ok || accepted.Value.IsSome || err != nil || !called {
		t.Fatalf("unsupported script result: %v, %v", result, err)
	}
}
