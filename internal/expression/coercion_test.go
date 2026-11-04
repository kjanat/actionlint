package expression

import "testing"

func TestSuspiciousCoercion(t *testing.T) {
	for _, tc := range []struct {
		input    string
		warnings int
	}{
		{"contains(null, '')", 1},
		{"startsWith(true, 'TR')", 1},
		{"endsWith('true', true)", 1},
		{"hashFiles('foo', null)", 1},
		{"fromJSON('[]') == 0", 1},
		{"fromJSON('{}') >= 0", 1},
		{"true > false", 1},
		{"null <= 0", 1},
		{"contains('hello', 'ell')", 0},
		{"contains(fromJSON('[true]'), true)", 0},
		{"endsWith(711, '11')", 0},
		{"fromJSON('{}') == fromJSON('{}')", 0},
		{"fromJSON('[]') == fromJSON('[1]')", 0},
		{"github.event.unknown >= 0", 0},
		{"startsWith(github.event.unknown, 'x')", 0},
	} {
		for _, enabled := range []bool{false, true} {
			t.Run(tc.input+map[bool]string{false: "/off", true: "/on"}[enabled], func(t *testing.T) {
				node, err := NewParser().Parse(NewLexer(tc.input + "}}"))
				if err != nil {
					t.Fatal(err)
				}
				checker := NewChecker(false, &Options{Checks: Checks{SuspiciousCoercion: enabled}})
				checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
				_, findings := checker.Check(node)
				want := 0
				if enabled {
					want = tc.warnings
				}
				if len(findings) != want {
					t.Fatalf("findings=%v, want %d", findings, want)
				}
				for _, finding := range findings {
					if finding.Rule != "coercion" || finding.Code != "coercion" || finding.EndOffset <= finding.Offset {
						t.Fatalf("invalid warning: %+v", finding)
					}
				}
			})
		}
	}
}

// Predicate and join behavior follows actions/runner at 1ba885c2065fd9be317808ab3fd406f392635cf7.
func TestFunctionCallFacts(t *testing.T) {
	for _, tc := range []struct {
		input string
		kind  Kind
		valid bool
		state EvaluationState
		value any
	}{
		{"startsWith(true, 'TR')", KindBool, true, EvaluationKnown, true},
		{"contains(null, '')", KindBool, true, EvaluationKnown, true},
		{"startsWith(fromJSON('{}'), 'x')", KindBool, true, EvaluationKnown, false},
		{"contains('foo', fromJSON('{}'))", KindBool, true, EvaluationKnown, false},
		{"startsWith(fromJSON('{}'), fromJSON(null))", KindBool, true, EvaluationKnown, false},
		{"contains(fromJSON('[]'), fromJSON(null))", KindBool, true, EvaluationKnown, false},
		{"startsWith(fromJSON('{}'), startsWith(true))", KindBool, false, EvaluationNotAttempted, nil},
		{"startsWith('x', fromJSON(null))", KindBool, false, EvaluationInvalid, nil},
		{"join(fromJSON('{}'))", KindString, true, EvaluationKnown, ""},
		{"format(true)", KindString, true, EvaluationKnown, "true"},
		{"format(fromJSON('{}'))", KindString, true, EvaluationKnown, "Object"},
		{"format(fromJSON('[]'))", KindString, true, EvaluationKnown, "Array"},
		{"fromJSON(fromJSON('{}'))", KindAny, false, EvaluationInvalid, nil},
		{"format('plain text', 1)", KindString, true, EvaluationKnown, "plain text"},
		{"fromJSON(true)", KindBool, true, EvaluationKnown, true},
		{"fromJSON(null)", KindAny, false, EvaluationInvalid, nil},
		{"startsWith(true)", KindBool, false, EvaluationNotAttempted, nil},
		{"hashFiles(fromJSON('{}'))", KindString, true, EvaluationUnknown, nil},
		{"startsWith(github.event.unknown, 'x')", KindBool, true, EvaluationUnknown, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.input + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(false, nil)
			checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
			got := checker.CheckResult(node)
			if got.Type.Kind() != tc.kind || got.Valid != tc.valid || got.Evaluation.State != tc.state || got.Evaluation.Value != tc.value {
				t.Fatalf("got type %s, valid %v, evaluation %+v, findings %v; want %s, %v, %v, %v", got.Type, got.Valid, got.Evaluation, got.Findings, tc.kind, tc.valid, tc.state, tc.value)
			}
			if tc.valid && tc.state == EvaluationKnown {
				checkGitHubReferenceLive(t, tc.input, nil, tc.value)
			}
		})
	}
}

func TestFunctionContractFacts(t *testing.T) {
	check := func(input string) ExpressionCheckResult {
		t.Helper()
		node, err := NewParser().Parse(NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		checker := NewChecker(false, nil)
		checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
		return checker.CheckResult(node)
	}
	got := check("fromJSON(true)")
	if got.Call == nil || len(got.Call.DeclaredReturns) != 1 || got.Call.DeclaredReturns[0].Kind() != KindAny || len(got.Call.RefinedAlternatives) != 1 || got.Call.RefinedAlternatives[0].Kind() != KindBool {
		t.Fatalf("fromJSON facts: %+v", got.Call)
	}
	if got.Call.ReturnDomain != ReturnDomainContainsValue {
		t.Fatalf("fromJSON return domain: %+v", got.Call)
	}
	arg := got.Call.Arguments[0]
	if arg.Source.Kind() != KindBool || arg.Parameter.Kind() != KindString || arg.Converted.Kind() != KindString || arg.Conversion != ConversionString || arg.SourceValue.Value != true || arg.Value.Value != "true" || !arg.Accepted {
		t.Fatalf("conversion facts: %+v", arg)
	}
	got = check("hashFiles(fromJSON('{}'))")
	if got.Call == nil || len(got.Call.DeclaredReturns) != 1 || got.Call.DeclaredReturns[0].Kind() != KindString || len(got.Call.Arguments) != 1 || !got.Call.Arguments[0].Accepted || got.Call.Arguments[0].Value.Value != "Object" {
		t.Fatalf("hashFiles call facts: %+v", got.Call)
	}
	got = check("noSuchFunction()")
	if got.Call != nil {
		t.Fatalf("unexpected contract: %+v", got.Call)
	}
	got = check("case(github.event_name == 'push', 1, 'fallback')")
	if got.Type.Kind() != KindAny || got.Call == nil || len(got.Call.RefinedAlternatives) != 2 || got.Call.RefinedAlternatives[0].Kind() != KindNumber || got.Call.RefinedAlternatives[1].Kind() != KindString {
		t.Fatalf("case alternatives: %+v", got.Call)
	}
}

func TestCallReturnsSurviveInvalidArguments(t *testing.T) {
	node, err := NewParser().Parse(NewLexer("startsWith(true).foo}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(false, nil)
	got := checker.CheckResult(node)
	if len(got.Findings) != 2 || got.Findings[0].Code != "argument-count" || got.Findings[1].Code != "operand-type" {
		t.Fatalf("findings: %v", got.Findings)
	}
}

func TestOverloadDeclaredAlternatives(t *testing.T) {
	checker := NewChecker(false, nil)
	checker.funcs = map[string][]*FuncSignature{"test": {
		{Name: "test", Ret: AnyType(), Params: []Type{StringType()}},
		{Name: "test", Ret: BoolType(), Params: []Type{BoolType()}},
	}}
	node, err := NewParser().Parse(NewLexer("test(github.event.unknown)}}"))
	if err != nil {
		t.Fatal(err)
	}
	got := checker.CheckResult(node)
	if got.Call == nil || len(got.Call.DeclaredReturns) != 2 || got.Call.DeclaredReturns[0].Kind() != KindAny || got.Call.DeclaredReturns[1].Kind() != KindBool || got.Type.Kind() != KindAny {
		t.Fatalf("result: %+v", got)
	}
}

func TestReturnValueOutsideDeclaredDomain(t *testing.T) {
	checker := NewChecker(false, nil)
	checker.funcs = map[string][]*FuncSignature{"startswith": {{Name: "startsWith", Ret: NumberType(), Params: []Type{StringType(), StringType()}}}}
	node, err := NewParser().Parse(NewLexer("startsWith('x', 'x')}}"))
	if err != nil {
		t.Fatal(err)
	}
	got := checker.CheckResult(node)
	if got.Call == nil || got.Call.ReturnDomain != ReturnDomainExcludesValue || got.Call.DeclaredReturns[0].Kind() != KindNumber || got.Evaluation.Value != true {
		t.Fatalf("result: %+v", got)
	}
}

func TestValidationStateIndependentOfFindings(t *testing.T) {
	parse := func(input string) Node {
		t.Helper()
		node, err := NewParser().Parse(NewLexer(input + "}}"))
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	checker := NewChecker(false, nil)
	checker.callFacts = make(map[*FuncCallNode]FunctionCallFacts)
	checker.check(parse("startsWith(true)"))
	if !checker.validationFailed || len(checker.errs) == 0 {
		t.Fatalf("missing validation failure: %+v", checker.errs)
	}
	checker.errs = nil // Simulate filtering the emitted finding.
	if !checker.validationFailed {
		t.Fatal("filtering findings changed validation")
	}

	checker = NewChecker(true, &Options{Checks: Checks{SuspiciousCoercion: true}})
	checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
	result := checker.CheckResult(parse("startsWith(true, github.event.issue.title)"))
	if !result.Valid || len(result.Findings) != 1 {
		t.Fatalf("advisory finding changed validation: %+v", result)
	}
	result = checker.CheckResult(parse("github.event.issue.title"))
	if !result.Valid || len(result.Findings) == 0 {
		t.Fatalf("security finding changed validation: %+v", result)
	}
}

func TestReturnDomainUsesNamedPropertyBeforeFallback(t *testing.T) {
	var shape ObjectBuilder
	shape.SetProperty("label", StringType())
	shape.SetMapped(NumberType())
	declared := shape.Build().Type()
	if !returnContainsValue(declared, map[string]any{"label": "x", "count": float64(1)}) {
		t.Fatal("valid object rejected")
	}
	if returnContainsValue(declared, map[string]any{"label": float64(1)}) {
		t.Fatal("wrong named property accepted")
	}
	if returnContainsValue(BoolType(), float64(1)) {
		t.Fatal("number accepted as boolean")
	}
}

func TestKnownArgumentRefinesUnknownSourceType(t *testing.T) {
	match := matchParameter(StringType(), callArgument{source: AnyType(), state: argumentKnown, value: true}, false)
	if match.argument.source.Kind() != KindAny || match.converted.Kind() != KindString || match.conversion != ConversionString || match.value != "true" {
		t.Fatalf("match: %+v", match)
	}
}

func TestKnownResultRefinesDeclaredAny(t *testing.T) {
	checker := NewChecker(false, nil)
	checker.funcs = map[string][]*FuncSignature{"startswith": {{Name: "startsWith", Ret: AnyType(), Params: []Type{StringType(), StringType()}}}}
	node, err := NewParser().Parse(NewLexer("startsWith('x', 'x')}}"))
	if err != nil {
		t.Fatal(err)
	}
	result := checker.CheckResult(node)
	if result.Call == nil || result.Call.DeclaredReturns[0].Kind() != KindAny || len(result.Call.RefinedAlternatives) != 1 || result.Call.RefinedAlternatives[0].Kind() != KindBool || result.Call.ReturnDomain != ReturnDomainContainsValue {
		t.Fatalf("result: %+v", result)
	}
}
