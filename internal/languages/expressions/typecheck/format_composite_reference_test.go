package typecheck

import (
	"testing"

	runnerreference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

func TestFormatCompositeReference(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"format('{0}', fromJSON('{}'))", "Object"},
		{"format('{0}', fromJSON('[]'))", "Array"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := checkCallDemandResult(t, tc.input)
			if !got.Valid || got.Evaluation.State != exprvalues.EvaluationKnown || got.Evaluation.Value != tc.want {
				t.Fatalf("wrong composite formatting: %+v", got)
			}
			runnerreference.CheckValue(t, tc.input, nil, tc.want)
		})
	}
}
