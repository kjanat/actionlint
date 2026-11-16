package typecheck

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/contexts"
)

type suppliedScope struct {
	variables    map[string]exprtypes.Type
	names        []string
	availability map[string]functions.Availability
	overloads    map[string][]*functions.FuncSignature
}

func (scope suppliedScope) NarrowContext(syntax.Node, bool, func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool) {
	return nil, true
}

type branchScope struct{ suppliedScope }

func (scope branchScope) NarrowContext(guard syntax.Node, truth bool, lookup func(string) (exprtypes.Type, bool)) (map[string]exprtypes.Type, bool) {
	if syntax.DisplayReference(guard) != "flag" {
		return nil, true
	}
	if _, found := lookup("subject"); !found {
		return nil, true
	}
	fields := map[string]exprtypes.Type{"number": exprtypes.NumberType()}
	if !truth {
		fields = map[string]exprtypes.Type{"text": exprtypes.StringType()}
	}
	return map[string]exprtypes.Type{"SUBJECT": exprtypes.NewStrictObjectType(fields).Type()}, true
}

func TestBranchRefinementUsesSuppliedModel(t *testing.T) {
	t.Parallel()
	original := exprtypes.NewEmptyStrictObjectType()
	scope := branchScope{suppliedScope{
		variables: map[string]exprtypes.Type{"flag": exprtypes.BoolType(), "subject": original.Type()},
		names:     []string{"flag", "subject"},
	}}
	checker := NewChecker(scope, nil)
	for _, input := range []string{"flag && subject.number > 0", "flag || subject.text != ''", "flag && (subject.number > 0 || subject.number == 0)"} {
		node := syntax.ParseAssignedExpression("${{ " + input + " }}")
		if node == nil {
			t.Fatal("expression did not parse")
		}
		if result := checker.CheckResult(evaluate.EvaluateSubexpressions(node)); !result.Valid || result.Type.Kind() != exprtypes.KindBool {
			t.Fatalf("%s: model refinement was not applied: %+v", input, result)
		}
		current, found := checker.ContextType("subject")
		if !found || !current.Equal(original.Type()) {
			t.Fatalf("branch refinement escaped its branch: %s", current)
		}
	}
	if result := checker.CheckResult(evaluate.EvaluateSubexpressions(syntax.ParseAssignedExpression("${{ subject.number }}"))); result.Valid {
		t.Fatal("branch-only property became globally available")
	}
}

func (scope suppliedScope) ContextTypes() map[string]exprtypes.Type { return scope.variables }
func (scope suppliedScope) ContextNames() []string                  { return scope.names }
func (scope suppliedScope) FunctionAvailability() map[string]functions.Availability {
	return scope.availability
}

func (scope suppliedScope) FunctionOverloads() map[string][]*functions.FuncSignature {
	return scope.overloads
}

func TestCallerScopeIsIndependentOfWorkflowMetadata(t *testing.T) {
	t.Parallel()
	scope := suppliedScope{
		variables:    map[string]exprtypes.Type{"counter": exprtypes.NumberType()},
		names:        []string{"counter"},
		availability: map[string]functions.Availability{"always": {AvailableAt: []string{"caller-selected location"}}},
		overloads: map[string][]*functions.FuncSignature{
			"shape": {{Name: "shape", Ret: exprtypes.NumberType()}},
			"echo":  {{Name: "echo", Ret: exprtypes.NumberType(), Params: []exprtypes.Type{exprtypes.StringType()}}},
		},
	}
	checker := NewChecker(scope, nil)
	allowedNames := []string{"counter"}
	checker.SetContextAvailability(allowedNames)
	allowedNames[0] = "changed"
	scope.variables["counter"] = exprtypes.BoolType()
	scope.names[0] = "other"
	scope.availability["always"].AvailableAt[0] = "changed location"
	scope.availability["always"] = functions.Availability{Allowed: true}
	scope.overloads["shape"][0].Ret = exprtypes.BoolType()
	scope.overloads["shape"][0].Params = []exprtypes.Type{exprtypes.StringType()}
	scope.overloads["echo"][0].Params[0] = exprtypes.BoolType()
	for _, input := range []string{"counter", "shape()", "echo('value')"} {
		node := syntax.ParseAssignedExpression("${{ " + input + " }}")
		result := checker.CheckResult(evaluate.EvaluateSubexpressions(node))
		if !result.Valid || result.Type.Kind() != exprtypes.KindNumber {
			t.Fatalf("%s: %+v, want caller-supplied numeric type", input, result)
		}
	}
	typo := checker.CheckResult(evaluate.EvaluateSubexpressions(syntax.ParseAssignedExpression("${{ shap() }}")))
	if len(typo.Findings) != 1 || !strings.Contains(typo.Findings[0].Message, `did you mean "shape"?`) {
		t.Fatalf("scope function missing from name suggestions: %+v", typo)
	}
	result := checker.CheckResult(evaluate.EvaluateSubexpressions(syntax.ParseAssignedExpression("${{ always() }}")))
	if result.Valid || len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Message, "caller-selected location") || strings.Contains(result.Findings[0].Message, "changed location") {
		t.Fatalf("function restriction changed with the caller's map: %+v", result)
	}
	result = checker.CheckResult(evaluate.EvaluateSubexpressions(syntax.ParseAssignedExpression("${{ github }}")))
	if result.Valid {
		t.Fatal("checker invented a GitHub context absent from its supplied scope")
	}
}

func TestAvailabilityChangesPreserveInferredContextTypes(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.SetContextType("matrix", exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"target": exprtypes.NumberType()}).Type())
	checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.if"))
	call := syntax.ParseAssignedExpression("${{ success('build') }}")
	if result := checker.CheckResult(evaluate.EvaluateSubexpressions(call)); !result.Valid {
		t.Fatalf("job overload rejected job argument: %+v", result)
	}
	checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	if result := checker.CheckResult(evaluate.EvaluateSubexpressions(call)); result.Valid {
		t.Fatal("job-only overload leaked into step availability")
	}
	result := checker.CheckResult(evaluate.EvaluateSubexpressions(syntax.ParseAssignedExpression("${{ matrix.target }}")))
	if !result.Valid || result.Type.Kind() != exprtypes.KindNumber {
		t.Fatalf("availability replacement changed inferred matrix type: %+v", result)
	}
}

func TestContextTypeUpdatesDoNotGrantAvailability(t *testing.T) {
	t.Parallel()
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.SetContextType("CuStOm", exprtypes.NumberType())
	if value, found := checker.ContextType("CUSTOM"); !found || value.Kind() != exprtypes.KindNumber {
		t.Fatalf("context binding=(%s,%t), want numeric type", value, found)
	}
	if _, found := checker.ContextType("missing"); found {
		t.Fatal("missing context was reported as declared")
	}
	expression := syntax.ParseAssignedExpression("${{ custom }}")
	result := checker.CheckResult(evaluate.EvaluateSubexpressions(expression))
	if result.Valid || len(result.Findings) != 1 || result.Type.Kind() != exprtypes.KindNumber {
		t.Fatalf("binding granted availability or lost its declared type: %+v", result)
	}
	checker.SetContextAvailability([]string{"custom"})
	if result := checker.CheckResult(evaluate.EvaluateSubexpressions(expression)); !result.Valid || result.Type.Kind() != exprtypes.KindNumber {
		t.Fatalf("explicitly enabled context was rejected: %+v", result)
	}
}

func TestWorkflowFunctionScopesAgainstRunner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, key, expression, condition string
		valid                            bool
	}{
		{"job status arguments", "jobs.<job_id>.if", "success('build')", "    if: success('build')\n    steps:\n      - run: echo ok", true},
		{"step status arguments", "jobs.<job_id>.steps.if", "success('build')", "    steps:\n      - if: success('build')\n        run: echo ok", false},
		{"job filesystem function", "jobs.<job_id>.if", "hashFiles('**') != ''", "    if: hashFiles('**') != ''\n    steps:\n      - run: echo ok", false},
		{"step filesystem function", "jobs.<job_id>.steps.if", "hashFiles('**') != ''", "    steps:\n      - if: hashFiles('**') != ''\n        run: echo ok", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checker := NewChecker(contexts.WorkflowExpressionScope(tc.key), nil)
			expression := syntax.ParseAssignedExpression("${{ " + tc.expression + " }}")
			if expression == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(evaluate.EvaluateSubexpressions(expression))
			if result.Valid != tc.valid {
				t.Fatalf("valid=%t findings=%v, want %t", result.Valid, result.Findings, tc.valid)
			}
			workflow := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n%s\n", tc.condition)
			expressions.CheckWorkflow(t, workflow, tc.valid)
		})
	}
}

func TestAvailableContextsDetachSnapshot(t *testing.T) {
	t.Parallel()
	checker := NewChecker(suppliedScope{variables: map[string]exprtypes.Type{"subject": exprtypes.NewEmptyObjectType().Type()}, names: []string{"subject"}}, nil)
	contexts := checker.AvailableContexts()
	contexts["subject"] = exprtypes.StringType()
	if current, _ := checker.ContextType("subject"); current.Kind() != exprtypes.KindObject {
		t.Fatal("caller changed the checker's context")
	}
	snapshot := checker.AvailableContexts()
	checker.SetContextType("subject", exprtypes.NumberType())
	if snapshot["subject"].Kind() != exprtypes.KindObject {
		t.Fatal("scope update changed a published context snapshot")
	}
}
