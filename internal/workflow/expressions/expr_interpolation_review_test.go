package expressions

import (
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestExpressionFailurePhases(t *testing.T) {
	for _, tc := range []struct {
		source string
		phase  exprlang.FailurePhase
		state  exprlang.EvaluationState
		kind   exprlang.Kind
	}{
		{"${{ startsWith(true) }}", exprlang.FailureStaticValidation, exprlang.EvaluationNotAttempted, exprlang.KindBool},
		{"${{ startsWith(fromJSON(null), 'x') }}", exprlang.FailureEvaluation, exprlang.EvaluationInvalid, exprlang.KindBool},
		{"${{ case(1, 'a', 'b') }}", exprlang.FailureEvaluation, exprlang.EvaluationInvalid, exprlang.KindAny},
	} {
		t.Run(tc.source, func(t *testing.T) {
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
	rule := newTestChecker()
	source := "${{ startsWith('only-one') }}"
	ty, valid := rule.checkOneExpression(ownedStringFixture(&workflowlang.String{Value: source, Pos: &workflowlang.Pos{Line: 1, Col: 1}}), "value", "env")
	if valid || ty.Kind() != exprlang.KindBool {
		t.Fatalf("invalid call must retain its declared bool result: type=%s valid=%v", ty, valid)
	}
}

func TestExpressionPreservesCallFacts(t *testing.T) {
	exprlang.BuiltinFuncSignatures["hosttest"] = []*exprlang.FuncSignature{
		{Name: "hosttest", Ret: exprlang.AnyType(), Params: []exprlang.Type{exprlang.StringType()}},
		{Name: "hosttest", Ret: exprlang.BoolType(), Params: []exprlang.Type{exprlang.BoolType()}},
	}
	t.Cleanup(func() { delete(exprlang.BuiltinFuncSignatures, "hosttest") })
	for _, tc := range []struct {
		source   string
		declared []exprlang.Kind
		refined  exprlang.Kind
	}{
		{"${{ fromJSON(true) }}", []exprlang.Kind{exprlang.KindAny}, exprlang.KindBool},
		{"${{ hosttest(github.event.unknown) }}", []exprlang.Kind{exprlang.KindAny, exprlang.KindBool}, exprlang.KindAny},
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
	original := exprlang.BuiltinFuncSignatures["startswith"]
	exprlang.BuiltinFuncSignatures["startswith"] = []*exprlang.FuncSignature{{Name: "startsWith", Ret: exprlang.NumberType(), Params: []exprlang.Type{exprlang.StringType(), exprlang.StringType()}}}
	t.Cleanup(func() { exprlang.BuiltinFuncSignatures["startswith"] = original })
	rule := newTestChecker()
	types, valid := rule.checkExprsIn("${{ startsWith('x', 'x') }}", &workflowlang.Pos{Line: 1, Col: 1}, false, false, "env")
	if !valid || len(types) != 1 || types[0].ty.Kind() != exprlang.KindNumber || types[0].semantics.Call == nil || types[0].semantics.Call.ReturnDomain != exprlang.ReturnDomainExcludesValue {
		t.Fatalf("contradictory evaluated value replaced return contract: %+v", types)
	}
}

func TestAdvisoryDoesNotInvalidateExpression(t *testing.T) {
	rule := newTestChecker()
	rule.options.Semantics.Checks.SuspiciousCoercion = true
	source := "${{ startsWith(true, 'TR') }}"
	types, valid := rule.checkExprsIn(source, &workflowlang.Pos{Line: 1, Col: 1}, false, false, "env")
	if !valid || len(types) != 1 || types[0].ty.Kind() != exprlang.KindBool || types[0].semantics.Type.Kind() != exprlang.KindBool {
		t.Fatalf("advisory must preserve validity and result types: valid=%v types=%+v", valid, types)
	}
	if len(rule.Findings()) != 1 || rule.Findings()[0].Rule != "coercion" {
		t.Fatalf("expected coercion advisory: %+v", rule.Findings())
	}
	if types[0].semantics.Evaluation.State != exprlang.EvaluationKnown || types[0].semantics.Evaluation.Value != true {
		t.Fatalf("advisory lost constant evaluation: %+v", types[0].semantics.Evaluation)
	}
}

func TestAdvisoryDoesNotHideScriptInterpolation(t *testing.T) {
	rule := newTestChecker()
	rule.options.Semantics.Checks.SuspiciousCoercion = true
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
