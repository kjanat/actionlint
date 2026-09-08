package actionlint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestActionExpressionFunctions(t *testing.T) {
	tests := []struct {
		name, field, expression string
		bare                    bool
		want                    []actionExpressionViolation
	}{
		{"bare condition", "runs.steps.*.if", "success() && !cancelled()", true, nil},
		{"interpolated condition", "runs.steps.*.if", "${{ always() || failure() }}", true, nil},
		{"status outside condition", "runs.steps.*.name", "${{ success() }}", false,
			[]actionExpressionViolation{{message: `calls function "success" which is not available here`}}},
		{"status in default", "inputs.*.default", "${{ always() }}", false,
			[]actionExpressionViolation{{message: `calls function "always" which is not available here`}}},
		{"hash with no patterns", "inputs.*.default", "${{ hashFiles() }}", false,
			[]actionExpressionViolation{{message: `calls function "hashfiles" with 0 argument(s); expected at least 1 and at most 255`}}},
		{"status with argument", "runs.steps.*.if", "${{ success('x') }}", true,
			[]actionExpressionViolation{{message: `calls function "success" with 1 argument(s); expected at least 0 and at most 0`}}},
		{"case insensitive nested functions", "runs.steps.*.run", "${{ format('{0}', HASHFILES('*.go')) }}", false, nil},
		{"caller properties have no invented types", "runs.steps.*.run", "${{ inputs.custom }} ${{ steps.custom.outputs.custom }}", false, nil},
		{"quoted delimiters", "runs.steps.*.name", "${{ '}} ${{ success() }}' }}", false, nil},
		{"literal then real function", "runs.steps.*.name", "${{ '}} ${{ success() }}' }}${{ success() }}", false,
			[]actionExpressionViolation{{message: `calls function "success" which is not available here`}}},
		{"deduplicated functions", "inputs.*.default", "${{ always() }}${{ ALWAYS() }}", false,
			[]actionExpressionViolation{{message: `calls function "always" which is not available here`}}},
		{"hash maximum", "runs.steps.*.shell", "${{ hashFiles(" + strings.Repeat("'x',", 254) + "'x') }}", false, nil},
		{"hash over maximum", "runs.steps.*.shell", "${{ hashFiles(" + strings.Repeat("'x',", 255) + "'x') }}", false,
			[]actionExpressionViolation{{message: `calls function "hashfiles" with 256 argument(s); expected at least 1 and at most 255`}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, actionExpressionViolations(tc.expression, tc.bare, tc.field), cmp.AllowUnexported(actionExpressionViolation{})); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
