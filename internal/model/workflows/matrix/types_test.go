package matrix

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestContextTypeUsesReachabilityAndIncludeCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, matrix string
		kind         types.TypeKind
		open         bool
	}{
		{"known axis", `item: [1, 2]`, types.KindNumber, false},
		{"excluded string", `item: [1, text], exclude: [{item: text}]`, types.KindNumber, false},
		{"known include", `item: [1], include: [{item: text}]`, types.KindAny, false},
		{"inferred axis", `item: ["${{ inputs.value }}"]`, types.KindNumber, false},
		{"unknown exclude", `item: ["${{ inputs.value }}"], exclude: "${{ fromJSON(inputs.exclude) }}"`, types.KindNumber, false},
		{"unknown include", `item: ["${{ inputs.value }}"], include: "${{ fromJSON(inputs.include) }}"`, types.KindAny, true},
		{"unknown include with known axis", `item: [1], include: "${{ fromJSON(inputs.include) }}"`, types.KindAny, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow, errors := parser.ParseWorkflow([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {" + tc.matrix + "}\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := Resolve(workflow.Jobs["test"].Strategy)
			inferred := map[source.Position]types.Type{}
			for _, axis := range resolution.Axes() {
				for _, candidate := range axis.Values {
					inferred[candidate.Origin().Span.Start] = types.NumberType()
				}
			}
			context := resolution.ContextType(inferred)
			if ty, found := context.Property("item"); !found || ty.Kind() != tc.kind {
				t.Fatalf("item=%v, want %v", ty, tc.kind)
			}
			if _, open := context.Mapped(); open != tc.open {
				t.Fatalf("context=%v, want open=%t", context, tc.open)
			}
			clear(inferred)
			if ty, _ := context.Property("item"); ty.Kind() != tc.kind {
				t.Fatal("caller mutation changed the published context")
			}
		})
	}
}
