package typecheck

import (
	"testing"

	runnerreference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// Predicate and join behavior follows actions/runner at 1ba885c2065fd9be317808ab3fd406f392635cf7.
func TestFunctionCallFacts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		kind  exprtypes.Kind
		valid bool
		state exprvalues.EvaluationState
		value any
	}{
		{"startsWith(true, 'TR')", exprtypes.KindBool, true, exprvalues.EvaluationKnown, true},
		{"contains(null, '')", exprtypes.KindBool, true, exprvalues.EvaluationKnown, true},
		{"startsWith(fromJSON('{}'), 'x')", exprtypes.KindBool, true, exprvalues.EvaluationKnown, false},
		{"contains('foo', fromJSON('{}'))", exprtypes.KindBool, true, exprvalues.EvaluationKnown, false},
		{"startsWith(fromJSON('{}'), fromJSON(null))", exprtypes.KindBool, true, exprvalues.EvaluationKnown, false},
		{"contains(fromJSON('[]'), fromJSON(null))", exprtypes.KindBool, true, exprvalues.EvaluationKnown, false},
		{"startsWith(fromJSON('{}'), startsWith(true))", exprtypes.KindBool, false, exprvalues.EvaluationNotAttempted, nil},
		{"startsWith('x', fromJSON(null))", exprtypes.KindBool, false, exprvalues.EvaluationInvalid, nil},
		{"join(fromJSON('{}'))", exprtypes.KindString, true, exprvalues.EvaluationKnown, ""},
		{"format(true)", exprtypes.KindString, true, exprvalues.EvaluationKnown, "true"},
		{"format(fromJSON('{}'))", exprtypes.KindString, true, exprvalues.EvaluationKnown, "Object"},
		{"format(fromJSON('[]'))", exprtypes.KindString, true, exprvalues.EvaluationKnown, "Array"},
		{"fromJSON(fromJSON('{}'))", exprtypes.KindAny, false, exprvalues.EvaluationInvalid, nil},
		{"format('plain text', 1)", exprtypes.KindString, true, exprvalues.EvaluationKnown, "plain text"},
		{"fromJSON(true)", exprtypes.KindBool, true, exprvalues.EvaluationKnown, true},
		{"fromJSON(null)", exprtypes.KindAny, false, exprvalues.EvaluationInvalid, nil},
		{"startsWith(true)", exprtypes.KindBool, false, exprvalues.EvaluationNotAttempted, nil},
		{"hashFiles(fromJSON('{}'))", exprtypes.KindString, true, exprvalues.EvaluationUnknown, nil},
		{"startsWith(github.event.unknown, 'x')", exprtypes.KindBool, true, exprvalues.EvaluationUnknown, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
			checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
			got := checker.CheckResult(expreval.EvaluateSubexpressions(node))
			if got.Type.Kind() != tc.kind || got.Valid != tc.valid || got.Evaluation.State != tc.state || got.Evaluation.Value != tc.value {
				t.Fatalf("got type %s, valid %v, evaluation %+v, findings %v; want %s, %v, %v, %v", got.Type, got.Valid, got.Evaluation, got.Findings, tc.kind, tc.valid, tc.state, tc.value)
			}
			if tc.valid && tc.state == exprvalues.EvaluationKnown {
				runnerreference.CheckValue(t, tc.input, nil, tc.value)
			}
		})
	}
}

func TestFunctionContractFacts(t *testing.T) {
	t.Parallel()
	check := func(input string) ExpressionCheckResult {
		t.Helper()
		node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
		checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
		return checker.CheckResult(expreval.EvaluateSubexpressions(node))
	}
	got := check("fromJSON(true)")
	if got.Call == nil || len(got.Call.DeclaredReturns) != 1 || got.Call.DeclaredReturns[0].Kind() != exprtypes.KindAny || len(got.Call.RefinedAlternatives) != 1 || got.Call.RefinedAlternatives[0].Kind() != exprtypes.KindBool {
		t.Fatalf("fromJSON facts: %+v", got.Call)
	}
	if got.Call.ReturnDomain != exprfunctions.ReturnDomainContainsValue {
		t.Fatalf("fromJSON return domain: %+v", got.Call)
	}
	arg := got.Call.Arguments[0]
	if arg.Source.Kind() != exprtypes.KindBool || arg.Parameter.Kind() != exprtypes.KindString || arg.Converted.Kind() != exprtypes.KindString || arg.Conversion != exprfunctions.ConversionString || arg.SourceValue.Value != true || arg.Value.Value != "true" || !arg.Accepted {
		t.Fatalf("conversion facts: %+v", arg)
	}
	got = check("hashFiles(fromJSON('{}'))")
	if got.Call == nil || len(got.Call.DeclaredReturns) != 1 || got.Call.DeclaredReturns[0].Kind() != exprtypes.KindString || len(got.Call.Arguments) != 1 || !got.Call.Arguments[0].Accepted || got.Call.Arguments[0].Value.Value != "Object" {
		t.Fatalf("hashFiles call facts: %+v", got.Call)
	}
	got = check("noSuchFunction()")
	if got.Call != nil {
		t.Fatalf("unexpected contract: %+v", got.Call)
	}
	got = check("case(github.event_name == 'push', 1, 'fallback')")
	if got.Type.Kind() != exprtypes.KindAny || got.Call == nil || len(got.Call.RefinedAlternatives) != 2 || got.Call.RefinedAlternatives[0].Kind() != exprtypes.KindNumber || got.Call.RefinedAlternatives[1].Kind() != exprtypes.KindString {
		t.Fatalf("case alternatives: %+v", got.Call)
	}
}

func TestCallReturnsSurviveInvalidArguments(t *testing.T) {
	t.Parallel()
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("startsWith(true).foo}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	got := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	if len(got.Findings) != 2 || got.Findings[0].Code != "argument-count" || got.Findings[1].Code != "operand-type" {
		t.Fatalf("findings: %v", got.Findings)
	}
}

func TestOverloadDeclaredAlternatives(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.funcs = map[string][]*exprfunctions.FuncSignature{"test": {
		{Name: "test", Ret: exprtypes.AnyType(), Params: []exprtypes.Type{exprtypes.StringType()}},
		{Name: "test", Ret: exprtypes.BoolType(), Params: []exprtypes.Type{exprtypes.BoolType()}},
	}}
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("test(github.event.unknown)}}"))
	if err != nil {
		t.Fatal(err)
	}
	got := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	if got.Call == nil || len(got.Call.DeclaredReturns) != 2 || got.Call.DeclaredReturns[0].Kind() != exprtypes.KindAny || got.Call.DeclaredReturns[1].Kind() != exprtypes.KindBool || got.Type.Kind() != exprtypes.KindAny {
		t.Fatalf("result: %+v", got)
	}
}

func TestReturnValueOutsideDeclaredDomain(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.funcs = map[string][]*exprfunctions.FuncSignature{"startswith": {{Name: "startsWith", Ret: exprtypes.NumberType(), Params: []exprtypes.Type{exprtypes.StringType(), exprtypes.StringType()}}}}
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("startsWith('x', 'x')}}"))
	if err != nil {
		t.Fatal(err)
	}
	got := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	if got.Call == nil || got.Call.ReturnDomain != exprfunctions.ReturnDomainExcludesValue || got.Call.DeclaredReturns[0].Kind() != exprtypes.KindNumber || got.Evaluation.Value != true {
		t.Fatalf("result: %+v", got)
	}
}

func TestValidationStateIndependentOfFindings(t *testing.T) {
	t.Parallel()
	parse := func(input string) exprsyntax.Node {
		t.Helper()
		node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.CheckResult(expreval.EvaluateSubexpressions(parse("startsWith(true)")))
	if !checker.validationFailed || len(checker.errs) == 0 {
		t.Fatalf("missing validation failure: %+v", checker.errs)
	}
	checker.errs = nil // Simulate filtering the emitted finding.
	if !checker.validationFailed {
		t.Fatal("filtering findings changed validation")
	}

	checker = NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	result := checker.CheckResult(expreval.EvaluateSubexpressions(parse("startsWith(true, github.event.issue.title)")))
	if !result.Valid || len(result.Findings) != 0 {
		t.Fatalf("type checking emitted a coercion policy finding: %+v", result)
	}
	result = checker.CheckResult(expreval.EvaluateSubexpressions(parse("github.event.issue.title")))
	if !result.Valid || len(result.Findings) != 0 {
		t.Fatalf("type checking emitted a script security policy finding: %+v", result)
	}
}

func TestReturnDomainUsesNamedPropertyBeforeFallback(t *testing.T) {
	t.Parallel()
	var shape exprtypes.ObjectBuilder
	shape.SetProperty("label", exprtypes.StringType())
	shape.SetMapped(exprtypes.NumberType())
	declared := shape.Build().Type()
	if !returnContainsValue(declared, map[string]any{"label": "x", "count": float64(1)}) {
		t.Fatal("valid object rejected")
	}
	if returnContainsValue(declared, map[string]any{"label": float64(1)}) {
		t.Fatal("wrong named property accepted")
	}
	if returnContainsValue(exprtypes.BoolType(), float64(1)) {
		t.Fatal("number accepted as boolean")
	}
}

func TestKnownArgumentRefinesUnknownSourceType(t *testing.T) {
	t.Parallel()
	match := exprfunctions.MatchParameter(exprtypes.StringType(), exprfunctions.CallArgument{Source: exprtypes.AnyType(), State: exprfunctions.ArgumentKnown, Value: true}, false)
	if match.Argument.Source.Kind() != exprtypes.KindAny || match.Converted.Kind() != exprtypes.KindString || match.Conversion != exprfunctions.ConversionString || match.Value != "true" {
		t.Fatalf("match: %+v", match)
	}
}

func TestKnownResultRefinesDeclaredAny(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.funcs = map[string][]*exprfunctions.FuncSignature{"startswith": {{Name: "startsWith", Ret: exprtypes.AnyType(), Params: []exprtypes.Type{exprtypes.StringType(), exprtypes.StringType()}}}}
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer("startsWith('x', 'x')}}"))
	if err != nil {
		t.Fatal(err)
	}
	result := checker.CheckResult(expreval.EvaluateSubexpressions(node))
	if result.Call == nil || result.Call.DeclaredReturns[0].Kind() != exprtypes.KindAny || len(result.Call.RefinedAlternatives) != 1 || result.Call.RefinedAlternatives[0].Kind() != exprtypes.KindBool || result.Call.ReturnDomain != exprfunctions.ReturnDomainContainsValue {
		t.Fatalf("result: %+v", result)
	}
}
