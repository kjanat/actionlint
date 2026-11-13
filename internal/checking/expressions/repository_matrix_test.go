package expressions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

func TestExpressionRefinementStates(t *testing.T) {
	t.Parallel()
	checker := newTestChecker()
	for _, tc := range []struct {
		name       string
		evaluation exprvalues.EvaluationResult
		want       value.State
	}{
		{"known null", exprvalues.EvaluationResult{State: exprvalues.EvaluationKnown, Value: nil}, value.Evaluated},
		{"unknown", exprvalues.EvaluationResult{State: exprvalues.EvaluationUnknown}, value.Dynamic},
		{"invalid", exprvalues.EvaluationResult{State: exprvalues.EvaluationInvalid}, value.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checker.expressionType("${{ env.value }}", tc.evaluation)
			if got.state != tc.want || got.hasTemplateInvalid {
				t.Fatalf("refinement=%+v, want state %v without invalid template row", got, tc.want)
			}
			if tc.name == "known null" && got.ty.Kind() != exprtypes.KindNull {
				t.Fatalf("known null type lost: %v", got.ty)
			}
		})
	}
}

func TestKP2BWMatrixExpressionTypes(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join("..", "..", "development", "tests", "integration", "workflow", "testdata", "repositories", "kp2bw-windows-bw-cmd.yml"))
	if err != nil {
		t.Fatal(err)
	}
	checker, workflow := newTestCheckerForSource(t, string(source))
	job := testJob(t, workflow, "cmd-shim")
	for _, err := range []error{checker.VisitWorkflowPre(workflow), checker.VisitJobPre(job)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"pm", "cli", "tools"} {
		inferred, exists := checker.matrixTy.Property(name)
		if !exists || inferred.Kind() != exprtypes.KindString {
			t.Fatalf("matrix.%s: got %v, want string", name, inferred)
		}
	}
	resolution := checker.matrixResolution(job.Strategy())
	if tools := resolution.Reachable("tools"); !tools.Complete || !tools.MayBeMissing || len(tools.Values) != 1 {
		t.Fatalf("include-only tools knowledge lost: %+v", tools)
	}
	action, ok := testStep(t, job, 1).Exec().Action()
	if !ok {
		t.Fatal("mise action not recognized")
	}
	insertions := slices.Collect(action.InputInsertions())
	if len(insertions) != 1 {
		t.Fatalf("input insertions: %v", insertions)
	}
	inferred, valid := checker.checkOneExpression(insertions[0].Expression(), "mise inputs", "jobs.<job_id>.steps.with")
	object, isObject := inferred.Object()
	if !valid || !isObject || !object.IsStrict() {
		t.Fatalf("dynamic inputs lost their closed object type: %v", inferred)
	}
	installArgs, exists := object.Property("install_args")
	if !exists || installArgs.Kind() != exprtypes.KindString {
		t.Fatalf("install_args type: %v", installArgs)
	}
	findings := checker.Findings()
	if len(findings) != 1 || findings[0].Severity != "info" || findings[0].Code != "permissive-json" || findings[0].Message != "you naughty boy, I see you" || findings[0].Start.Line != 24 {
		t.Fatalf("expected only the loose JSON information: %v", findings)
	}
}

func TestFiniteMatrixObjectCannotBeInterpolatedAsString(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		`fromJSON(format('{{"value":"{0}"}}', matrix.item))`,
		`matrix.item == 'one' && fromJSON('{}') || 'ok'`,
		`matrix.item == 'one' && fromJSON('{}') || env.TOKEN`,
		`matrix.item == 'one' && fromJSON('{}') || null`,
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			checker, workflow := newTestCheckerForSource(t, `on: push
jobs:
  check:
    runs-on: ubuntu-latest
    strategy:
      matrix: {item: [one, two]}
    steps:
      - name: >-
          prefix ${{ `+expression+` }}
        run: echo ok
`)
			job := testJob(t, workflow, "check")
			for _, err := range []error{checker.VisitWorkflowPre(workflow), checker.VisitJobPre(job), checker.VisitStep(testStep(t, job, 0))} {
				if err != nil {
					t.Fatal(err)
				}
			}
			findings := checker.Findings()
			if len(findings) != 1 || !strings.Contains(findings[0].Message, "object and array values should not be evaluated in template") {
				t.Fatalf("finite object became string-compatible: %v", findings)
			}
		})
	}
}
