package expressions

import (
	"strings"
	"testing"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestExpressionFailurePhases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		source string
		phase  exprlang.FailurePhase
		state  exprvalues.EvaluationState
		kind   exprtypes.Kind
	}{
		{"${{ startsWith(true) }}", exprlang.FailureStaticValidation, exprvalues.EvaluationNotAttempted, exprtypes.KindBool},
		{"${{ startsWith(fromJSON(null), 'x') }}", exprlang.FailureEvaluation, exprvalues.EvaluationInvalid, exprtypes.KindBool},
		{"${{ case(1, 'a', 'b') }}", exprlang.FailureEvaluation, exprvalues.EvaluationInvalid, exprtypes.KindAny},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			rule := newTestChecker()
			types, valid := rule.checkExprsIn(tc.source, &workflowlang.Pos{Line: 1, Col: 1}, false, false, "env")
			if valid || len(types) != 1 {
				t.Fatalf("unexpected expression results: valid=%v results=%+v", valid, types)
			}
			got := types[0]
			if got.ty.Kind() != tc.kind || got.semantics.FailurePhase != tc.phase || got.semantics.Evaluation.State != tc.state || got.refinement.state != value.Invalid {
				t.Fatalf("lost phase or type facts: %+v", got)
			}
		})
	}
}

func TestInvalidCallRetainsReturnType(t *testing.T) {
	t.Parallel()
	rule := newTestChecker()
	source := "${{ startsWith('only-one') }}"
	ty, valid := rule.checkOneExpression(ownedStringFixture(&workflowlang.String{Value: source, Pos: &workflowlang.Pos{Line: 1, Col: 1}}), "value", "env")
	if valid || ty.Kind() != exprtypes.KindBool {
		t.Fatalf("invalid call must retain its declared bool result: type=%s valid=%v", ty, valid)
	}
}

func TestExpressionPreservesCallFacts(t *testing.T) {
	exprfunctions.Builtins["hosttest"] = exprfunctions.Function{Signatures: []*exprfunctions.FuncSignature{
		{Name: "hosttest", Ret: exprtypes.AnyType(), Params: []exprtypes.Type{exprtypes.StringType()}},
		{Name: "hosttest", Ret: exprtypes.BoolType(), Params: []exprtypes.Type{exprtypes.BoolType()}},
	}}
	t.Cleanup(func() { delete(exprfunctions.Builtins, "hosttest") })
	for _, tc := range []struct {
		source   string
		declared []exprtypes.Kind
		refined  exprtypes.Kind
	}{
		{"${{ fromJSON(true) }}", []exprtypes.Kind{exprtypes.KindAny}, exprtypes.KindBool},
		{"${{ hosttest(github.event.unknown) }}", []exprtypes.Kind{exprtypes.KindAny, exprtypes.KindBool}, exprtypes.KindAny},
	} {
		rule := newTestChecker()
		types, valid := rule.checkExprsIn(tc.source, &workflowlang.Pos{Line: 1, Col: 1}, false, false, "env")
		if !valid || len(types) != 1 || types[0].semantics.Call == nil {
			t.Fatalf("missing call facts for %q: %+v", tc.source, types)
		}
		result := types[0].semantics
		if result.Type.Kind() != tc.refined || len(result.Call.DeclaredReturns) != len(tc.declared) {
			t.Fatalf("call facts for %q: %+v", tc.source, result)
		}
		for i, want := range tc.declared {
			if result.Call.DeclaredReturns[i].Kind() != want {
				t.Fatalf("declared returns for %q: %+v", tc.source, result.Call.DeclaredReturns)
			}
		}
	}
	original := exprfunctions.Builtins["startswith"]
	changed := original
	changed.Signatures = []*exprfunctions.FuncSignature{{Name: "startsWith", Ret: exprtypes.NumberType(), Params: []exprtypes.Type{exprtypes.StringType(), exprtypes.StringType()}}}
	exprfunctions.Builtins["startswith"] = changed
	t.Cleanup(func() { exprfunctions.Builtins["startswith"] = original })
	rule := newTestChecker()
	types, valid := rule.checkExprsIn("${{ startsWith('x', 'x') }}", &workflowlang.Pos{Line: 1, Col: 1}, false, false, "env")
	if !valid || len(types) != 1 || types[0].ty.Kind() != exprtypes.KindNumber || types[0].semantics.Call == nil || types[0].semantics.Call.ReturnDomain != exprfunctions.ReturnDomainExcludesValue {
		t.Fatalf("contradictory evaluated value replaced return contract: %+v", types)
	}
}

func TestAdvisoryDoesNotInvalidateExpression(t *testing.T) {
	t.Parallel()
	rule := newTestChecker()
	rule.options.SuspiciousCoercion = true
	source := "${{ startsWith(true, 'TR') }}"
	types, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, false, "env")
	if !valid || len(types) != 1 || types[0].ty.Kind() != exprtypes.KindBool || types[0].semantics.Type.Kind() != exprtypes.KindBool {
		t.Fatalf("advisory must preserve validity and result types: valid=%v types=%+v", valid, types)
	}
	if len(rule.Findings()) != 1 || rule.Findings()[0].Rule != "coercion" {
		t.Fatalf("expected coercion advisory: %+v", rule.Findings())
	}
	if types[0].semantics.Evaluation.State != exprvalues.EvaluationKnown || types[0].semantics.Evaluation.Value != true {
		t.Fatalf("advisory lost constant evaluation: %+v", types[0].semantics.Evaluation)
	}
}

func TestAdvisoryDoesNotHideScriptInterpolation(t *testing.T) {
	t.Parallel()
	rule := newTestChecker()
	rule.options.SuspiciousCoercion = true
	source := "${{ startsWith(true, 'TR') && github.event.issue.title }}"
	_, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, true, "jobs.<job_id>.steps.run")
	if !valid {
		t.Fatal("advisory or script finding invalidated expression")
	}
	seen := map[string]bool{}
	for _, finding := range rule.Findings() {
		seen[finding.Rule] = true
	}
	if !seen["coercion"] || !seen["script-injection"] {
		t.Fatalf("expected both findings: %+v", rule.Findings())
	}
}

func TestInterpolationSemanticErrorsRemainInvalid(t *testing.T) {
	t.Parallel()
	for _, script := range []bool{false, true} {
		for _, source := range []string{
			"${{ unknown.value }} ${{ github.ref && '' || 'fallback' }}",
			"${{ github.ref && '' || 'fallback' }} ${{ unknown.value }}",
			"${{ unknown.first }} ${{ unknown.second }} ${{ github.ref && '' || 'fallback' }}",
			"${{ missingFunction('}}') }} ${{ github.ref && '' || 'fallback' }}",
		} {
			rule := newTestChecker()
			types, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, script, "env")
			if valid || len(types) != strings.Count(source, "${{") {
				t.Fatalf("must scan all parsed interpolations without treating their types as valid: %v, %+v", valid, types)
			}
			ternaries := 0
			for _, finding := range rule.Findings() {
				if finding.Rule == "unsound-ternary" {
					ternaries++
				}
			}
			if ternaries != 1 {
				t.Fatalf("later diagnostic missing: %+v", rule.Findings())
			}
			if ty, ok := rule.checkOneExpression(ownedStringFixture(&workflowlang.String{Value: source, Pos: &workflowlang.Pos{Line: 1, Col: 1}}), "value", "env"); ok {
				t.Fatalf("invalid interpolation types escaped into type checking: %v", ty)
			}
		}
	}
}

func TestInterpolationParseErrorsStillStopScanning(t *testing.T) {
	t.Parallel()
	for _, script := range []bool{false, true} {
		for _, source := range []string{
			"${{ github.ref + }} ${{ github.ref && '' || 'fallback' }}",
			"${{ 'unterminated }} ${{ github.ref && '' || 'fallback' }}",
			"${{ unknown.value",
		} {
			rule := newTestChecker()
			types, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, script, "env")
			if valid || types != nil || len(rule.Findings()) == 0 {
				t.Fatalf("unparsed interpolation must remain invalid: %v, %+v, %+v", valid, types, rule.Findings())
			}
			for _, finding := range rule.Findings() {
				if finding.Rule != "expression-syntax" {
					t.Fatalf("must not resume from an unparsed expression boundary: %+v", finding)
				}
			}
		}
	}
}
