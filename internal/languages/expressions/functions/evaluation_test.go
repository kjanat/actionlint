package functions

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

type recordedContext struct {
	arguments []values.EvaluationResult
	seen      map[int]bool
	order     []int
}

func (context *recordedContext) ArgumentCount() int { return len(context.arguments) }
func (context *recordedContext) Argument(index int) values.EvaluationResult {
	if !context.seen[index] {
		context.seen[index] = true
		context.order = append(context.order, index)
	}
	return context.arguments[index]
}

func TestBuiltinImplementationsRequestRequiredArguments(t *testing.T) {
	t.Parallel()
	bad := invalidValue()
	for _, tc := range []struct {
		name, function string
		args           []values.EvaluationResult
		want           values.EvaluationResult
		order          []int
	}{
		{"array membership is exact", "contains", []values.EvaluationResult{knownValue([]any{"push", "pull_request"}), knownValue("pull_request_target")}, knownValue(false), []int{0, 1}},
		{"empty array skips item", "contains", []values.EvaluationResult{knownValue([]any{}), bad}, knownValue(false), []int{0}},
		{"object skips pattern", "startswith", []values.EvaluationResult{knownValue(map[string]any{}), bad}, knownValue(false), []int{0}},
		{"predicate coerces boolean", "startswith", []values.EvaluationResult{knownValue(true), knownValue("TR")}, knownValue(true), []int{0, 1}},
		{"scalar join skips separator", "join", []values.EvaluationResult{knownValue("single"), bad}, knownValue("single"), []int{0}},
		{"one item skips separator", "join", []values.EvaluationResult{knownValue([]any{"single"}), bad}, knownValue("single"), []int{0}},
		{"join reads separator", "join", []values.EvaluationResult{knownValue([]any{"a", "b"}), knownValue(":")}, knownValue("a:b"), []int{0, 1}},
		{"join composite separator", "join", []values.EvaluationResult{knownValue([]any{"a", "b"}), knownValue(map[string]any{})}, knownValue("a,b"), []int{0, 1}},
		{"false case skips value", "case", []values.EvaluationResult{knownValue(false), bad, knownValue("fallback")}, knownValue("fallback"), []int{0, 2}},
		{"true case skips default", "case", []values.EvaluationResult{knownValue(true), knownValue("selected"), bad}, knownValue("selected"), []int{0, 1}},
		{"case requires boolean", "case", []values.EvaluationResult{knownValue("true"), bad, bad}, bad, []int{0}},
		{"format follows placeholders", "format", []values.EvaluationResult{knownValue("{1}{0}{1}"), knownValue("zero"), knownValue("one")}, knownValue("onezeroone"), []int{0, 2, 1}},
		{"format checks syntax first", "format", []values.EvaluationResult{knownValue("{0} {"), bad}, bad, []int{0}},
		{"format reads value before specifier", "format", []values.EvaluationResult{knownValue("{0:bad}"), knownValue("value")}, bad, []int{0, 1}},
		{"invalid JSON is failure", "fromjson", []values.EvaluationResult{knownValue("{")}, bad, []int{0}},
		{"JSON zero is mode dependent", "fromjson", []values.EvaluationResult{knownValue("-0")}, unknownValue(), []int{0}},
		{"JSON collisions are unknown", "fromjson", []values.EvaluationResult{knownValue(`{"A":1,"a":2}`)}, unknownValue(), []int{0}},
		{"JSON string", "tojson", []values.EvaluationResult{knownValue("safe")}, knownValue(`"safe"`), []int{0}},
		{"JSON escaping is mode dependent", "tojson", []values.EvaluationResult{knownValue("<")}, unknownValue(), []int{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			context := &recordedContext{arguments: tc.args, seen: map[int]bool{}}
			got := Builtins[tc.function].Evaluate(context)
			if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(context.order, tc.order) {
				t.Fatalf("result=%+v order=%v, want %+v order=%v", got, context.order, tc.want, tc.order)
			}
		})
	}
}

func TestBuiltinArityAndUnsupportedEvaluation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		count int
		state values.EvaluationState
	}{
		{"format", 0, values.EvaluationNotAttempted},
		{"case", 4, values.EvaluationNotAttempted},
		{"case", 257, values.EvaluationNotAttempted},
		{"hashfiles", 1, values.EvaluationUnknown},
	} {
		context := &recordedContext{arguments: make([]values.EvaluationResult, tc.count), seen: map[int]bool{}}
		if got := Builtins[tc.name].Evaluate(context); got.State != tc.state || len(context.order) != 0 {
			t.Fatalf("%s/%d: result=%+v order=%v", tc.name, tc.count, got, context.order)
		}
	}
}
