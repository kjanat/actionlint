package typecheck

import (
	"testing"

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
			checkGitHubReferenceLive(t, tc.input, nil, tc.want)
		})
	}
}
