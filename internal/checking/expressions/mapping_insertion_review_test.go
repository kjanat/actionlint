package expressions

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestEvaluatedMatrixKeyIsData(t *testing.T) {
	t.Parallel()
	source := &definition.String{Value: "${{ missing }}", Pos: &sourcetext.Position{Line: 1, Column: 1}}
	row := &definition.MatrixRow{Name: source, Evaluated: true, Values: []definition.RawYAMLValue{&definition.RawYAMLString{Value: "one", Position: source.Pos}}}
	rule := newTestChecker()
	rule.checkMatrixRow(matrixRowFixture(row))
	if errs := rule.Findings(); len(errs) != 0 {
		t.Fatalf("evaluated key interpreted as source: %v", errs)
	}
}
