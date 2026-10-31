package expressions

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestEvaluatedMatrixKeyIsData(t *testing.T) {
	source := &workflowlang.String{Value: "${{ missing }}", Pos: &workflowlang.Pos{Line: 1, Col: 1}}
	row := &workflowlang.MatrixRow{Name: source, Evaluated: true, Values: []workflowlang.RawYAMLValue{&workflowlang.RawYAMLString{Value: "one", Position: source.Pos}}}
	rule := newTestChecker()
	rule.checkMatrixRow(ownedMatrixRowFixture(row))
	if errs := rule.Findings(); len(errs) != 0 {
		t.Fatalf("evaluated key interpreted as source: %v", errs)
	}
}
