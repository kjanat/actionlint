package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// Predicate and join behavior follows actions/runner at 1ba885c2065fd9be317808ab3fd406f392635cf7.
func TestFunctionCallFacts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		kind  types.TypeKind
		valid bool
		state values.EvaluationState
		value any
	}{
		{"startsWith(true, 'TR')", types.KindBool, true, values.EvaluationKnown, true},
		{"contains(null, '')", types.KindBool, true, values.EvaluationKnown, true},
		{"startsWith(fromJSON('{}'), 'x')", types.KindBool, true, values.EvaluationKnown, false},
		{"contains('foo', fromJSON('{}'))", types.KindBool, true, values.EvaluationKnown, false},
		{"startsWith(fromJSON('{}'), fromJSON(null))", types.KindBool, true, values.EvaluationKnown, false},
		{"contains(fromJSON('[]'), fromJSON(null))", types.KindBool, true, values.EvaluationKnown, false},
		{"startsWith(fromJSON('{}'), startsWith(true))", types.KindBool, false, values.EvaluationNotAttempted, nil},
		{"startsWith('x', fromJSON(null))", types.KindBool, false, values.EvaluationInvalid, nil},
		{"join(fromJSON('{}'))", types.KindString, true, values.EvaluationKnown, ""},
		{"format(true)", types.KindString, true, values.EvaluationKnown, "true"},
		{"format(fromJSON('{}'))", types.KindString, true, values.EvaluationKnown, "Object"},
		{"format(fromJSON('[]'))", types.KindString, true, values.EvaluationKnown, "Array"},
		{"fromJSON(fromJSON('{}'))", types.KindAny, false, values.EvaluationInvalid, nil},
		{"format('plain text', 1)", types.KindString, true, values.EvaluationKnown, "plain text"},
		{"fromJSON(true)", types.KindBool, true, values.EvaluationKnown, true},
		{"fromJSON(null)", types.KindAny, false, values.EvaluationInvalid, nil},
		{"startsWith(true)", types.KindBool, false, values.EvaluationNotAttempted, nil},
		{"hashFiles(fromJSON('{}'))", types.KindString, true, values.EvaluationUnknown, nil},
		{"startsWith(github.event.unknown, 'x')", types.KindBool, true, values.EvaluationUnknown, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(contexts.DefaultExpressionScope(), nil)
			checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			got := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
			if got.Type.Kind() != tc.kind || got.Valid != tc.valid || got.Evaluation.State != tc.state || got.Evaluation.Value != tc.value {
				t.Fatalf("got type %s, valid %v, evaluation %+v, findings %v; want %s, %v, %v, %v", got.Type, got.Valid, got.Evaluation, got.Findings, tc.kind, tc.valid, tc.state, tc.value)
			}
			if tc.valid && tc.state == values.EvaluationKnown {
				expressions.CheckValue(t, tc.input, nil, tc.value)
			}
		})
	}
}

func TestFunctionContractFacts(t *testing.T) {
	t.Parallel()
	check := func(input string) ExpressionCheckResult {
		t.Helper()
		node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		checker := NewChecker(contexts.DefaultExpressionScope(), nil)
		checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
		return checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	}
	got := check("fromJSON(true)")
	if got.Call == nil || len(got.Call.DeclaredReturns) != 1 || got.Call.DeclaredReturns[0].Kind() != types.KindAny || len(got.Call.RefinedAlternatives) != 1 || got.Call.RefinedAlternatives[0].Kind() != types.KindBool {
		t.Fatalf("fromJSON facts: %+v", got.Call)
	}
	if got.Call.ReturnDomain != functions.ReturnDomainContainsValue {
		t.Fatalf("fromJSON return domain: %+v", got.Call)
	}
	arg := got.Call.Arguments[0]
	if arg.Source.Kind() != types.KindBool || arg.Parameter.Kind() != types.KindString || arg.Converted.Kind() != types.KindString || arg.Conversion != functions.ConversionString || arg.SourceValue.Value != true || arg.Value.Value != "true" || !arg.Accepted {
		t.Fatalf("conversion facts: %+v", arg)
	}
	got = check("hashFiles(fromJSON('{}'))")
	if got.Call == nil || len(got.Call.DeclaredReturns) != 1 || got.Call.DeclaredReturns[0].Kind() != types.KindString || len(got.Call.Arguments) != 1 || !got.Call.Arguments[0].Accepted || got.Call.Arguments[0].Value.Value != "Object" {
		t.Fatalf("hashFiles call facts: %+v", got.Call)
	}
	got = check("noSuchFunction()")
	if got.Call != nil {
		t.Fatalf("unexpected contract: %+v", got.Call)
	}
	got = check("case(github.event_name == 'push', 1, 'fallback')")
	if got.Type.Kind() != types.KindAny || got.Call == nil || len(got.Call.RefinedAlternatives) != 2 || got.Call.RefinedAlternatives[0].Kind() != types.KindNumber || got.Call.RefinedAlternatives[1].Kind() != types.KindString {
		t.Fatalf("case alternatives: %+v", got.Call)
	}
}

func TestCallReturnsSurviveInvalidArguments(t *testing.T) {
	t.Parallel()
	node, err := syntax.NewParser().ParseExpression(syntax.NewLexer("startsWith(true).foo}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	got := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	if len(got.Findings) != 2 || got.Findings[0].Code != "argument-count" || got.Findings[1].Code != "operand-type" {
		t.Fatalf("findings: %v", got.Findings)
	}
}

func TestOverloadDeclaredAlternatives(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.funcs = map[string][]*functions.FuncSignature{"test": {
		{Name: "test", Ret: types.AnyType(), Params: []types.Type{types.StringType()}},
		{Name: "test", Ret: types.BoolType(), Params: []types.Type{types.BoolType()}},
	}}
	node, err := syntax.NewParser().ParseExpression(syntax.NewLexer("test(github.event.unknown)}}"))
	if err != nil {
		t.Fatal(err)
	}
	got := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	if got.Call == nil || len(got.Call.DeclaredReturns) != 2 || got.Call.DeclaredReturns[0].Kind() != types.KindAny || got.Call.DeclaredReturns[1].Kind() != types.KindBool || got.Type.Kind() != types.KindAny {
		t.Fatalf("result: %+v", got)
	}
}

func TestReturnValueOutsideDeclaredDomain(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.funcs = map[string][]*functions.FuncSignature{"startswith": {{Name: "startsWith", Ret: types.NumberType(), Params: []types.Type{types.StringType(), types.StringType()}}}}
	node, err := syntax.NewParser().ParseExpression(syntax.NewLexer("startsWith('x', 'x')}}"))
	if err != nil {
		t.Fatal(err)
	}
	got := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	if got.Call == nil || got.Call.ReturnDomain != functions.ReturnDomainExcludesValue || got.Call.DeclaredReturns[0].Kind() != types.KindNumber || got.Evaluation.Value != true {
		t.Fatalf("result: %+v", got)
	}
}

func TestValidationStateIndependentOfFindings(t *testing.T) {
	t.Parallel()
	parse := func(input string) syntax.Node {
		t.Helper()
		node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.CheckResult(evaluate.EvaluateSubexpressions(parse("startsWith(true)")))
	if !checker.validationFailed || len(checker.errs) == 0 {
		t.Fatalf("missing validation failure: %+v", checker.errs)
	}
	checker.errs = nil // Simulate filtering the emitted finding.
	if !checker.validationFailed {
		t.Fatal("filtering findings changed validation")
	}

	checker = NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	result := checker.CheckResult(evaluate.EvaluateSubexpressions(parse("startsWith(true, github.event.issue.title)")))
	if !result.Valid || len(result.Findings) != 0 {
		t.Fatalf("type checking emitted a coercion policy finding: %+v", result)
	}
	result = checker.CheckResult(evaluate.EvaluateSubexpressions(parse("github.event.issue.title")))
	if !result.Valid || len(result.Findings) != 0 {
		t.Fatalf("type checking emitted a script security policy finding: %+v", result)
	}
}

func TestReturnDomainUsesNamedPropertyBeforeFallback(t *testing.T) {
	t.Parallel()
	var shape types.ObjectBuilder
	shape.SetProperty("label", types.StringType())
	shape.SetMapped(types.NumberType())
	declared := shape.Build().Type()
	if !returnContainsValue(declared, map[string]any{"label": "x", "count": float64(1)}) {
		t.Fatal("valid object rejected")
	}
	if returnContainsValue(declared, map[string]any{"label": float64(1)}) {
		t.Fatal("wrong named property accepted")
	}
	if returnContainsValue(types.BoolType(), float64(1)) {
		t.Fatal("number accepted as boolean")
	}
}

func TestKnownArgumentRefinesUnknownSourceType(t *testing.T) {
	t.Parallel()
	match := functions.MatchParameter(types.StringType(), functions.CallArgument{Source: types.AnyType(), State: functions.ArgumentKnown, Value: true}, false)
	if match.Argument.Source.Kind() != types.KindAny || match.Converted.Kind() != types.KindString || match.Conversion != functions.ConversionString || match.Value != "true" {
		t.Fatalf("match: %+v", match)
	}
}

func TestKnownResultRefinesDeclaredAny(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.funcs = map[string][]*functions.FuncSignature{"startswith": {{Name: "startsWith", Ret: types.AnyType(), Params: []types.Type{types.StringType(), types.StringType()}}}}
	node, err := syntax.NewParser().ParseExpression(syntax.NewLexer("startsWith('x', 'x')}}"))
	if err != nil {
		t.Fatal(err)
	}
	result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
	if result.Call == nil || result.Call.DeclaredReturns[0].Kind() != types.KindAny || len(result.Call.RefinedAlternatives) != 1 || result.Call.RefinedAlternatives[0].Kind() != types.KindBool || result.Call.ReturnDomain != functions.ReturnDomainContainsValue {
		t.Fatalf("result: %+v", result)
	}
}
