package expressions

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestEvaluatedMatrixKeyIsData(t *testing.T) {
	t.Parallel()
	source := &workflowlang.String{Value: "${{ missing }}", Pos: &source.Position{Line: 1, Column: 1}}
	row := &workflowlang.MatrixRow{Name: source, Evaluated: true, Values: []workflowlang.RawYAMLValue{&workflowlang.RawYAMLString{Value: "one", Position: source.Pos}}}
	rule := newTestChecker()
	rule.checkMatrixRow(ownedMatrixRowFixture(row))
	if errs := rule.Findings(); len(errs) != 0 {
		t.Fatalf("evaluated key interpreted as source: %v", errs)
	}
}
