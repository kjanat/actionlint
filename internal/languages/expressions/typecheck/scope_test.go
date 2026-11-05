package typecheck

import (
	"fmt"
	"strings"
	"testing"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
)

type suppliedScope struct {
	variables    map[string]exprtypes.Type
	names        []string
	availability map[string]exprfunctions.Availability
	overloads    map[string][]*exprfunctions.FuncSignature
}

func (scope suppliedScope) ContextTypes() map[string]exprtypes.Type { return scope.variables }
func (scope suppliedScope) ContextNames() []string                  { return scope.names }
func (scope suppliedScope) FunctionAvailability() map[string]exprfunctions.Availability {
	return scope.availability
}

func (scope suppliedScope) FunctionOverloads() map[string][]*exprfunctions.FuncSignature {
	return scope.overloads
}

func TestCallerScopeIsIndependentOfWorkflowMetadata(t *testing.T) {
	scope := suppliedScope{
		variables:    map[string]exprtypes.Type{"counter": exprtypes.NumberType()},
		names:        []string{"counter"},
		availability: map[string]exprfunctions.Availability{"always": {AvailableAt: []string{"caller-selected location"}}},
		overloads: map[string][]*exprfunctions.FuncSignature{
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
	scope.availability["always"] = exprfunctions.Availability{Allowed: true}
	scope.overloads["shape"][0].Ret = exprtypes.BoolType()
	scope.overloads["shape"][0].Params = []exprtypes.Type{exprtypes.StringType()}
	scope.overloads["echo"][0].Params[0] = exprtypes.BoolType()
	for _, input := range []string{"counter", "shape()", "echo('value')"} {
		node := exprsyntax.ParseAssignedExpression("${{ " + input + " }}")
		result := checker.CheckResult(node)
		if !result.Valid || result.Type.Kind() != exprtypes.KindNumber {
			t.Fatalf("%s: %+v, want caller-supplied numeric type", input, result)
		}
	}
	typo := checker.CheckResult(exprsyntax.ParseAssignedExpression("${{ shap() }}"))
	if len(typo.Findings) != 1 || !strings.Contains(typo.Findings[0].Message, `did you mean "shape"?`) {
		t.Fatalf("scope function missing from name suggestions: %+v", typo)
	}
	result := checker.CheckResult(exprsyntax.ParseAssignedExpression("${{ always() }}"))
	if result.Valid || len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Message, "caller-selected location") || strings.Contains(result.Findings[0].Message, "changed location") {
		t.Fatalf("function restriction changed with the caller's map: %+v", result)
	}
	result = checker.CheckResult(exprsyntax.ParseAssignedExpression("${{ github }}"))
	if result.Valid {
		t.Fatal("checker invented a GitHub context absent from its supplied scope")
	}
}

func TestAvailabilityChangesPreserveInferredContextTypes(t *testing.T) {
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetContextType("matrix", exprtypes.NewStrictObjectType(map[string]exprtypes.Type{"target": exprtypes.NumberType()}).Type())
	checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.if"))
	call := exprsyntax.ParseAssignedExpression("${{ success('build') }}")
	if result := checker.CheckResult(call); !result.Valid {
		t.Fatalf("job overload rejected job argument: %+v", result)
	}
	checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	if result := checker.CheckResult(call); result.Valid {
		t.Fatal("job-only overload leaked into step availability")
	}
	result := checker.CheckResult(exprsyntax.ParseAssignedExpression("${{ matrix.target }}"))
	if !result.Valid || result.Type.Kind() != exprtypes.KindNumber {
		t.Fatalf("availability replacement changed inferred matrix type: %+v", result)
	}
}

func TestContextTypeUpdatesDoNotGrantAvailability(t *testing.T) {
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetContextType("CuStOm", exprtypes.NumberType())
	if value, found := checker.ContextType("CUSTOM"); !found || value.Kind() != exprtypes.KindNumber {
		t.Fatalf("context binding=(%s,%t), want numeric type", value, found)
	}
	if _, found := checker.ContextType("missing"); found {
		t.Fatal("missing context was reported as declared")
	}
	expression := exprsyntax.ParseAssignedExpression("${{ custom }}")
	result := checker.CheckResult(expression)
	if result.Valid || len(result.Findings) != 1 || result.Type.Kind() != exprtypes.KindNumber {
		t.Fatalf("binding granted availability or lost its declared type: %+v", result)
	}
	checker.SetContextAvailability([]string{"custom"})
	if result := checker.CheckResult(expression); !result.Valid || result.Type.Kind() != exprtypes.KindNumber {
		t.Fatalf("explicitly enabled context was rejected: %+v", result)
	}
}

func TestWorkflowFunctionScopesAgainstRunner(t *testing.T) {
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
			checker := NewChecker(contextspec.WorkflowExpressionScope(tc.key), nil)
			expression := exprsyntax.ParseAssignedExpression("${{ " + tc.expression + " }}")
			if expression == nil {
				t.Fatal("expression did not parse")
			}
			result := checker.CheckResult(expression)
			if result.Valid != tc.valid {
				t.Fatalf("valid=%t findings=%v, want %t", result.Valid, result.Findings, tc.valid)
			}
			workflow := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n%s\n", tc.condition)
			checkGitHubWorkflowReference(t, workflow, tc.valid)
		})
	}
}
