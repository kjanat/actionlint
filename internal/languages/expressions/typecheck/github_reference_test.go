package typecheck

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	"actionlint.kjanat.dev/v2/internal/model/contexts"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"github.com/google/go-cmp/cmp"
)

// Examples and semantic boundaries from GitHub's expression reference, checked
// on 2026-11-04. Tests use local inputs and never fetch the page:
// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions
func TestGitHubReferenceValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		kind        types.TypeKind
		want        any
	}{
		{"literal null", "null", types.KindNull, nil},
		{"literal boolean", "false", types.KindBool, false},
		{"literal integer", "711", types.KindNumber, float64(711)},
		{"literal decimal", "-9.2", types.KindNumber, -9.2},
		{"literal hexadecimal", "0xff", types.KindNumber, float64(255)},
		{"literal exponent", "-2.99e-2", types.KindNumber, -0.0299},
		{"literal escaped quote", "'It''s open source!'", types.KindString, "It's open source!"},
		{"grouping and boolean operators", "!(false || true && false)", types.KindBool, true},
		{"array index", `fromJSON('["first", "second"]')[1]`, types.KindString, "second"},
		{"property dereference", `fromJSON('{"status":"success"}').status`, types.KindString, "success"},
		{"property index", `fromJSON('{"status":"success"}')['status']`, types.KindString, "success"},
		{"less", "1 < 2", types.KindBool, true},
		{"less equal", "2 <= 2", types.KindBool, true},
		{"greater", "2 > 1", types.KindBool, true},
		{"greater equal", "2 >= 2", types.KindBool, true},
		{"not equal", "1 != 2", types.KindBool, true},
		{"case insensitive equality", "'Hello' == 'HELLO'", types.KindBool, true},
		{"null numeric coercion", "null == 0", types.KindBool, true},
		{"true numeric coercion", "true == 1", types.KindBool, true},
		{"false numeric coercion", "false == 0", types.KindBool, true},
		{"empty string numeric coercion", "'' == 0", types.KindBool, true},
		{"numeric string coercion", "'711' == 711", types.KindBool, true},
		{"nonnumeric string equality", "'no number' == 0", types.KindBool, false},
		{"NaN less", "'no number' < 0", types.KindBool, false},
		{"NaN less equal", "'no number' <= 0", types.KindBool, false},
		{"NaN greater", "'no number' > 0", types.KindBool, false},
		{"NaN greater equal", "'no number' >= 0", types.KindBool, false},
		{"contains string", "contains('Hello world', 'llo')", types.KindBool, true},
		{"contains ignores case", "contains('Hello world', 'LLO')", types.KindBool, true},
		{"contains absent substring", "contains('Hello world', 'goodbye')", types.KindBool, false},
		{"contains fromJSON push", `contains(fromJSON('["push", "pull_request"]'), 'push')`, types.KindBool, true},
		{"contains fromJSON pull request", `contains(fromJSON('["push", "pull_request"]'), 'pull_request')`, types.KindBool, true},
		{"contains fromJSON other event", `contains(fromJSON('["push", "pull_request"]'), 'issues')`, types.KindBool, false},
		{"array membership is not substring matching", `contains(fromJSON('["push", "pull_request"]'), 'pull')`, types.KindBool, false},
		{"contains array ignores case", `contains(fromJSON('["push"]'), 'PUSH')`, types.KindBool, true},
		{"mixed case function names", `CoNtAiNs(fRoMjSoN('["push"]'), 'push')`, types.KindBool, true},
		{"number string coercion", "endsWith(711, '11')", types.KindBool, true},
		{"startsWith", "startsWith('Hello world', 'He')", types.KindBool, true},
		{"startsWith ignores case", "startsWith('Hello world', 'he')", types.KindBool, true},
		{"endsWith", "endsWith('Hello world', 'ld')", types.KindBool, true},
		{"endsWith ignores case", "endsWith('Hello world', 'LD')", types.KindBool, true},
		{"format", "format('Hello {0} {1} {2}', 'Mona', 'the', 'Octocat')", types.KindString, "Hello Mona the Octocat"},
		{"format escaped braces", "format('{{Hello {0} {1} {2}!}}', 'Mona', 'the', 'Octocat')", types.KindString, "{Hello Mona the Octocat!}"},
		{"join separator", `join(fromJSON('["bug", "help wanted"]'), ', ')`, types.KindString, "bug, help wanted"},
		{"join default separator", `join(fromJSON('["bug", "help wanted"]'))`, types.KindString, "bug,help wanted"},
		{"join string", "join('bug', ', ')", types.KindString, "bug"},
		{"toJSON scalar", "toJSON(false)", types.KindString, "false"},
		{"fromJSON boolean", "fromJSON('true')", types.KindBool, true},
		{"fromJSON integer", "fromJSON('3')", types.KindNumber, float64(3)},
		{"fromJSON numerical comparison", "fromJSON('10') > 3", types.KindBool, true},
		{"fromJSON string", `fromJSON('"hello"')`, types.KindString, "hello"},
		{"fromJSON null", "fromJSON('null')", types.KindNull, nil},
		{"fromJSON array", `fromJSON('["push", "pull_request"]')`, types.KindArray, []any{"push", "pull_request"}},
		{"fromJSON object", `fromJSON('{"status":"success"}')`, types.KindObject, map[string]any{"status": "success"}},
		{"case first match", "case(true, 'first', true, 'second', 'default')", types.KindString, "first"},
		{"case default", "case(false, 'first', false, 'second', 'default')", types.KindString, "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node, ty := checkGitHubReference(t, tc.input)
			if ty.Kind() != tc.kind {
				t.Fatalf("type = %s, want %s", ty, tc.kind)
			}
			got, known := evaluate.ConditionConstantValue(node)
			if !known {
				t.Fatal("documented constant was not evaluated")
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("value mismatch (-want +got):\n%s", diff)
			}
			expressions.CheckValue(t, tc.input, nil, tc.want)
		})
	}
}

func TestGitHubReferenceCoercions(t *testing.T) {
	t.Parallel()
	// Supported coercions infer bool without type errors, independently of warning selection.
	for _, tc := range []struct {
		input string
		want  bool
		kind  types.TypeKind
		kinds []findingKind
	}{
		{"fromJSON('[]') == 0", false, types.KindBool, nil},
		{"fromJSON('{}') >= 0", false, types.KindBool, nil},
		{"contains(null, '')", true, types.KindBool, nil},
		{"startsWith(true, 'TR')", true, types.KindBool, nil},
		{"true > false", true, types.KindBool, nil},
		{"null <= 0", true, types.KindBool, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, ty := checkGitHubReference(t, tc.input, tc.kinds...)
			got, known := evaluate.ConditionConstantValue(node)
			if ty.Kind() != tc.kind || !known || got != tc.want {
				t.Fatalf("type=%s value=%v known=%t, want type=%s value=%t", ty, got, known, tc.kind, tc.want)
			}
			expressions.CheckValue(t, tc.input, nil, tc.want)
		})
	}
}

func TestGitHubReferenceTruthiness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"false", false},
		{"0", false},
		{"-0", false},
		{"''", false},
		{"null", false},
		{"true", true},
		{"1", true},
		{"-1", true},
		{"'false'", true},
		{"'0'", true},
		{"fromJSON('[]')", true},
		{"fromJSON('{}')", true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, _ := checkGitHubReference(t, tc.input)
			got, known := evaluate.ConditionConstantValue(node)
			if !known || values.ExpressionTruthy(got) != tc.want {
				t.Fatalf("value=%v known=%t, want truthy=%t", got, known, tc.want)
			}
		})
	}
	if _, err := syntax.NewParser().ParseExpression(syntax.NewLexer(`"double quoted"}}`)); err == nil {
		t.Fatal("double-quoted expression string was accepted")
	}
}

func TestGitHubReferenceContextScenarios(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input, property, value string
		want                   any
	}{
		{`contains(fromJSON('["push", "pull_request"]'), github.event_name)`, "event_name", "push", true},
		{`contains(fromJSON('["push", "pull_request"]'), github.event_name)`, "event_name", "pull_request", true},
		{`contains(fromJSON('["push", "pull_request"]'), github.event_name)`, "event_name", "pull_request_target", false},
		{`contains(fromJSON('["push", "pull_request"]'), github.event_name)`, "event_name", "issues", false},
		{"case(github.ref == 'refs/heads/main', 'production', 'development')", "ref", "refs/heads/main", "production"},
		{"case(github.ref == 'refs/heads/main', 'production', 'development')", "ref", "refs/heads/topic", "development"},
		{"case(github.ref == 'refs/heads/main', 'production', github.ref == 'refs/heads/staging', 'staging', startsWith(github.ref, 'refs/heads/feature/'), 'development', 'unknown')", "ref", "refs/heads/main", "production"},
		{"case(github.ref == 'refs/heads/main', 'production', github.ref == 'refs/heads/staging', 'staging', startsWith(github.ref, 'refs/heads/feature/'), 'development', 'unknown')", "ref", "refs/heads/staging", "staging"},
		{"case(github.ref == 'refs/heads/main', 'production', github.ref == 'refs/heads/staging', 'staging', startsWith(github.ref, 'refs/heads/feature/'), 'development', 'unknown')", "ref", "refs/heads/feature/topic", "development"},
		{"case(github.ref == 'refs/heads/main', 'production', github.ref == 'refs/heads/staging', 'staging', startsWith(github.ref, 'refs/heads/feature/'), 'development', 'unknown')", "ref", "refs/heads/other", "unknown"},
	} {
		t.Run(tc.input+"/"+tc.value, func(t *testing.T) {
			t.Parallel()
			node, _ := checkGitHubReference(t, tc.input)
			if got, known := evaluate.ConditionConstantValue(node); known {
				t.Fatalf("invented context value: %v", got)
			}
			context := map[string]any{"github": map[string]any{tc.property: tc.value}}
			got, known := evaluate.ExpressionValue(node, func(name string) (any, bool) {
				v, ok := context[name]
				return v, ok
			})
			if !known || got != tc.want {
				t.Fatalf("value=%v known=%t, want %v", got, known, tc.want)
			}
			expressions.CheckValue(t, tc.input, context, tc.want)
		})
	}
}

func TestGitHubReferenceRuntimeExpressions(t *testing.T) {
	t.Parallel()
	// These expressions are valid, but the constant evaluator must not invent
	// filesystem contents, execution status, object identity, or JSON formatting.
	for _, tc := range []struct {
		input string
		kind  types.TypeKind
	}{
		{"contains(github.event.issue.labels.*.name, 'bug')", types.KindBool},
		{"join(github.event.issue.labels.*.name, ', ')", types.KindString},
		{"toJSON(job)", types.KindString},
		{"hashFiles('**/package-lock.json')", types.KindString},
		{"hashFiles('/src/*.js')", types.KindString},
		{"hashFiles('/lib/**/*.rb')", types.KindString},
		{"hashFiles('**/package-lock.json', '**/Gemfile.lock')", types.KindString},
		{"hashFiles('/lib/**/*.rb', '!/lib/foo/*.rb')", types.KindString},
		{"success()", types.KindBool},
		{"always()", types.KindBool},
		{"cancelled()", types.KindBool},
		{"failure()", types.KindBool},
		{"failure() && steps.demo.conclusion == 'failure'", types.KindBool},
		{"job.status", types.KindString},
		{"steps.demo.outputs.number", types.KindString},
		{"fromJSON(steps.demo.outputs.number) > 3", types.KindBool},
		{"fromJSON('{}') == fromJSON('{}')", types.KindBool},
		{"fromJSON('[]') == fromJSON('[]')", types.KindBool},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, ty := checkGitHubReference(t, tc.input)
			if ty.Kind() != tc.kind {
				t.Fatalf("type = %s, want %s", ty, tc.kind)
			}
			if got, known := evaluate.ConditionConstantValue(node); known {
				t.Fatalf("invented runtime result: %v", got)
			}
		})
	}
}

func TestGitHubReferenceObjectFilters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		elem  types.Type
	}{
		{`fromJSON('[{"name":"apple","quantity":1},{"name":"orange","quantity":2},{"name":"pear","quantity":1}]').*.name`, types.StringType()},
		{`fromJSON('{"scallions":{"ediblePortions":["roots","stalks"]},"beets":{"ediblePortions":["roots","stems","leaves"]},"artichokes":{"ediblePortions":["hearts","stems","leaves"]}}').*.ediblePortions`, types.AnyType()},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			_, ty := checkGitHubReference(t, tc.input)
			array, ok := ty.Array()
			if !ok || !array.Elem().Equal(tc.elem) {
				t.Fatalf("filter type = %s, want array of %s", ty, tc.elem)
			}
		})
	}
}

func checkGitHubReference(t *testing.T, input string, wantDiagnostics ...findingKind) (syntax.Node, types.Type) {
	t.Helper()
	node, err := syntax.NewParser().ParseExpression(syntax.NewLexer(input + "}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(contexts.DefaultExpressionScope(), nil)
	checker.SetAvailability(contexts.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	checker.SetContextType("steps", types.NewStrictObjectType(map[string]types.Type{
		"demo": types.NewStrictObjectType(map[string]types.Type{
			"conclusion": types.StringType(), "outputs": types.NewMapObjectType(types.StringType()).Type(),
		}).Type(),
	}).Type())
	ty, diagnostics := checker.Check(evaluate.EvaluateSubexpressions(node))
	if len(diagnostics) != len(wantDiagnostics) {
		t.Fatalf("diagnostics=%v, want %v", diagnostics, wantDiagnostics)
	}
	for i, diagnostic := range diagnostics {
		if diagnostic.Rule != wantDiagnostics[i].rule || diagnostic.Code != wantDiagnostics[i].code {
			t.Fatalf("diagnostic=%v, want %v", diagnostic, wantDiagnostics[i])
		}
	}
	return node, ty
}
