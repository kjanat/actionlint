package matrix

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestContextTypeUsesReachabilityAndIncludeCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, matrix string
		kind         exprtypes.Kind
		open         bool
	}{
		{"known axis", `item: [1, 2]`, exprtypes.KindNumber, false},
		{"excluded string", `item: [1, text], exclude: [{item: text}]`, exprtypes.KindNumber, false},
		{"known include", `item: [1], include: [{item: text}]`, exprtypes.KindAny, false},
		{"inferred axis", `item: ["${{ inputs.value }}"]`, exprtypes.KindNumber, false},
		{"unknown exclude", `item: ["${{ inputs.value }}"], exclude: "${{ fromJSON(inputs.exclude) }}"`, exprtypes.KindNumber, false},
		{"unknown include", `item: ["${{ inputs.value }}"], include: "${{ fromJSON(inputs.include) }}"`, exprtypes.KindAny, true},
		{"unknown include with known axis", `item: [1], include: "${{ fromJSON(inputs.include) }}"`, exprtypes.KindAny, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workflow, errors := workflowparser.Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix: {" + tc.matrix + "}\n    steps: [{run: echo ok}]\n"))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			resolution := Resolve(workflow.Jobs["test"].Strategy)
			inferred := map[sourcepos.Position]exprtypes.Type{}
			for _, axis := range resolution.Axes() {
				for _, candidate := range axis.Values {
					inferred[candidate.Origin().Span.Start] = exprtypes.NumberType()
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
