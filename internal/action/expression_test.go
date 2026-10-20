package action

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestActionExpressionFallbackDeduplication(t *testing.T) {
	for _, tc := range []struct {
		expression string
		want       int
		positioned bool
	}{
		{"${{ 'secrets.A || secrets.A' }}", 1, false},
		{"${{ 'secrets.A || vars.A' }}", 2, false},
		{"${{ 'unknown() || unknown()' }}", 1, false},
		{"${{ 'unknown() || other()' }}", 2, false},
		{"${{ secrets.A || secrets.A }}", 2, true},
		{"${{ unknown() || unknown() }}", 2, true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			got := ExpressionViolations(tc.expression, true, "runs.steps.*.if", nil)
			if len(got) != tc.want {
				t.Fatalf("want %d violations, got %+v", tc.want, got)
			}
			for _, violation := range got {
				if (violation.Offset != 0) != tc.positioned {
					t.Fatalf("position availability differs: %+v", violation)
				}
			}
			if tc.positioned && got[0].Offset == got[1].Offset {
				t.Fatal("distinct tokens share an offset")
			}
		})
	}
}

func TestActionExpressionContextSuggestions(t *testing.T) {
	for _, tc := range []struct {
		field, expression string
		want              []Violation
	}{
		{"runs.steps.*.run", "${{ github_repository }}", []Violation{{Message: `uses undefined variable "github_repository"; did you mean "github.repository"?`, Offset: 5}}},
		{"runs.steps.*.if", "GITHUB_REPOSITORY", []Violation{{Message: `uses undefined variable "GITHUB_REPOSITORY"; did you mean "github.repository"?`, Offset: 1}}},
		{"inputs.*.default", "${{ runner_os }}", []Violation{{Message: `uses undefined variable "runner_os"; did you mean "runner.os"?`, Offset: 5}}},
		{"runs.args.*", "${{ github_repository }}", []Violation{{Context: "github_repository"}}},
		{"runs.steps.*.run", "${{ secrets.TOKEN }}", []Violation{{Context: "secrets"}}},
		{"runs.steps.*.run", "${{ github_nonexistent }}", []Violation{{Context: "github_nonexistent"}}},
		{"runs.steps.*.run", "${{ inputs.github_token }} ${{ matrix.runner_os }}", nil},
	} {
		t.Run(tc.field+"/"+tc.expression, func(t *testing.T) {
			got := ExpressionViolations(tc.expression, tc.field == "runs.steps.*.if", tc.field, nil)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(Violation{}), cmpopts.IgnoreFields(Violation{}, "Offset", "EndOffset")); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestActionExpressionFunctions(t *testing.T) {
	tests := []struct {
		name, field, expression string
		bare                    bool
		want                    []Violation
	}{
		{"bare condition", "runs.steps.*.if", "success() && !cancelled()", true, nil},
		{"interpolated condition", "runs.steps.*.if", "${{ always() || failure() }}", true, nil},
		{
			"status outside condition", "runs.steps.*.name", "${{ success() }}", false,
			[]Violation{{Message: `calls function "success" which is not available here`}},
		},
		{
			"status in default", "inputs.*.default", "${{ always() }}", false,
			[]Violation{{Message: `calls function "always" which is not available here`}},
		},
		{
			"hash with no patterns", "inputs.*.default", "${{ hashFiles() }}", false,
			[]Violation{{Message: `calls function "hashfiles" with 0 argument(s); expected at least 1 and at most 255`}},
		},
		{
			"status with argument", "runs.steps.*.if", "${{ success('x') }}", true,
			[]Violation{{Message: `calls function "success" with 1 argument(s); expected at least 0 and at most 0`}},
		},
		{"case insensitive nested functions", "runs.steps.*.run", "${{ format('{0}', HASHFILES('*.go')) }}", false, nil},
		{"caller properties have no invented types", "runs.steps.*.run", "${{ inputs.custom }} ${{ steps.custom.outputs.custom }}", false, nil},
		{"quoted delimiters", "runs.steps.*.name", "${{ '}} ${{ success() }}' }}", false, nil},
		{
			"literal then real function", "runs.steps.*.name", "${{ '}} ${{ success() }}' }}${{ success() }}", false,
			[]Violation{{Message: `calls function "success" which is not available here`}},
		},
		{
			"repeated functions", "inputs.*.default", "${{ always() }}${{ ALWAYS() }}", false,
			[]Violation{{Message: `calls function "always" which is not available here`}, {Message: `calls function "always" which is not available here`}},
		},
		{"hash maximum", "runs.steps.*.shell", "${{ hashFiles(" + strings.Repeat("'x',", 254) + "'x') }}", false, nil},
		{
			"hash over maximum", "runs.steps.*.shell", "${{ hashFiles(" + strings.Repeat("'x',", 255) + "'x') }}", false,
			[]Violation{{Message: `calls function "hashfiles" with 256 argument(s); expected at least 1 and at most 255`}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ExpressionViolations(tc.expression, tc.bare, tc.field, nil), cmp.AllowUnexported(Violation{}), cmpopts.IgnoreFields(Violation{}, "Offset", "EndOffset")); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestActionConditionCompleteConsumption(t *testing.T) {
	for _, tc := range []struct {
		source  string
		invalid bool
	}{
		{"${{ 'github.ref }} ignored' }}", true},
		{"github.ref }} ignored", true},
		{"${{ 'github.ref }}' }}", true},
		{"${{ 'contains(github.ref, ''}}'')' }}", false},
		{"contains(github.ref, '}}')", false},
		{"${{ contains(github.ref, '}}') }}", false},
		{"${{ github.ref }} text ${{ github.actor }}", false},
		{"${{ '' }}", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			violations := ExpressionViolations(tc.source, true, "runs.steps.*.if", nil)
			if tc.invalid {
				if len(violations) != 1 || !strings.Contains(violations[0].Message, "unexpected trailing text in condition") {
					t.Fatalf("want trailing-text error, got %#v", violations)
				}
			} else if len(violations) != 0 {
				t.Fatalf("unexpected violations: %#v", violations)
			}
		})
	}
}

func TestCompositeStepUnavailableContexts(t *testing.T) {
	tests := []struct {
		what string
		expr string
		bare bool
		want []string
	}{
		{"secrets in interpolation", "echo ${{ secrets.TOKEN }}", false, []string{"secrets"}},
		{"vars in interpolation", "echo ${{ vars.FLAG }}", false, []string{"vars"}},
		{"needs in interpolation", "${{ needs.build.outputs.x }}", false, []string{"needs"}},
		{"bare if with secrets", "secrets.TOKEN != ''", true, []string{"secrets"}},
		{"bare if with vars", "vars.FLAG == 'true'", true, []string{"vars"}},
		{"multiple distinct contexts", "${{ secrets.A }}${{ vars.B }}", false, []string{"secrets", "vars"}},
		{"same context twice", "${{ secrets.A }}${{ secrets.B }}", false, []string{"secrets", "secrets"}},
		{"quoted expression marker", "${{ 'text }} ${{ secrets.TOKEN }}' }}", false, nil},
		{"literal followed by actual context", "${{ 'text }} ${{ secrets.TOKEN }}' }} ${{ vars.REGION }}", false, []string{"vars"}},
		{"escaped quote before marker", "${{ 'it''s }} ${{ secrets.TOKEN }}' }}", false, nil},
		{"multibyte literal followed by context", "${{ 'é }} ${{ secrets.TOKEN }}' }}${{ needs.build.result }}", false, []string{"needs"}},
		{"allowed contexts are clean", "${{ github.sha }} ${{ inputs.x }} ${{ steps.a.outputs.b }} ${{ env.E }} ${{ runner.os }} ${{ job.status }} ${{ matrix.m }} ${{ strategy.job-index }}", false, nil},
		{"no expression at all", "echo hello secrets.TOKEN", false, nil},
		{"bare literal", "true", true, nil},
		{"status function is not reported", "always()", true, nil},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			got := ExpressionViolations(tc.expr, tc.bare, "runs.steps.*.if", nil)
			var want []Violation
			for _, ctx := range tc.want {
				want = append(want, Violation{Context: ctx})
			}
			if diff := cmp.Diff(want, got, cmp.AllowUnexported(Violation{}), cmpopts.IgnoreFields(Violation{}, "Offset", "EndOffset")); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestActionInputDefaultUnavailableContexts(t *testing.T) {
	tests := []struct {
		what string
		expr string
		want []string
	}{
		{"step-only contexts", "${{ inputs.token }}${{ env.TOKEN }}${{ steps.previous.outputs.token }}", []string{"inputs", "env", "steps"}},
		{"unavailable action contexts", "${{ secrets.TOKEN }}${{ vars.REGION }}${{ needs.build.result }}", []string{"secrets", "vars", "needs"}},
		{"allowed contexts", "${{ github.sha }}${{ job.status }}${{ matrix.os }}${{ runner.os }}${{ strategy.job-index }}", nil},
		{"quoted marker followed by context", "${{ 'text }} ${{ inputs.token }}' }}${{ env.TOKEN }}", []string{"env"}},
		{"case-insensitive occurrences", "${{ INPUTS.a }}${{ inputs.b }}", []string{"inputs", "inputs"}},
		{"allowed function", "${{ format('{0}', hashFiles('*.go')) }}", nil},
	}
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			got := ExpressionViolations(tc.expr, false, "inputs.*.default", nil)
			var want []Violation
			for _, ctx := range tc.want {
				want = append(want, Violation{Context: ctx})
			}
			if diff := cmp.Diff(want, got, cmp.AllowUnexported(Violation{}), cmpopts.IgnoreFields(Violation{}, "Offset", "EndOffset")); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
