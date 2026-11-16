package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
)

func TestFormatCompositeReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, want string }{
		{"format('{0}', fromJSON('{}'))", "Object"},
		{"format('{0}', fromJSON('[]'))", "Array"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := checkCallDemandResult(t, tc.input)
			if !got.Valid || got.Evaluation.State != exprvalues.EvaluationKnown || got.Evaluation.Value != tc.want {
				t.Fatalf("wrong composite formatting: %+v", got)
			}
			expressions.CheckValue(t, tc.input, nil, tc.want)
		})
	}
}
