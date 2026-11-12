package typecheck

import (
	"testing"

	runnerreference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"github.com/google/go-cmp/cmp"
)

// Examples and semantic boundaries from GitHub's expression reference, checked
// on 2026-11-04. Tests use local inputs and never fetch the page:
// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions
func TestGitHubReferenceValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		kind        exprtypes.Kind
		want        any
	}{
		{"literal null", "null", exprtypes.KindNull, nil},
		{"literal boolean", "false", exprtypes.KindBool, false},
		{"literal integer", "711", exprtypes.KindNumber, float64(711)},
		{"literal decimal", "-9.2", exprtypes.KindNumber, -9.2},
		{"literal hexadecimal", "0xff", exprtypes.KindNumber, float64(255)},
		{"literal exponent", "-2.99e-2", exprtypes.KindNumber, -0.0299},
		{"literal escaped quote", "'It''s open source!'", exprtypes.KindString, "It's open source!"},
		{"grouping and boolean operators", "!(false || true && false)", exprtypes.KindBool, true},
		{"array index", `fromJSON('["first", "second"]')[1]`, exprtypes.KindString, "second"},
		{"property dereference", `fromJSON('{"status":"success"}').status`, exprtypes.KindString, "success"},
		{"property index", `fromJSON('{"status":"success"}')['status']`, exprtypes.KindString, "success"},
		{"less", "1 < 2", exprtypes.KindBool, true},
		{"less equal", "2 <= 2", exprtypes.KindBool, true},
		{"greater", "2 > 1", exprtypes.KindBool, true},
		{"greater equal", "2 >= 2", exprtypes.KindBool, true},
		{"not equal", "1 != 2", exprtypes.KindBool, true},
		{"case insensitive equality", "'Hello' == 'HELLO'", exprtypes.KindBool, true},
		{"null numeric coercion", "null == 0", exprtypes.KindBool, true},
		{"true numeric coercion", "true == 1", exprtypes.KindBool, true},
		{"false numeric coercion", "false == 0", exprtypes.KindBool, true},
		{"empty string numeric coercion", "'' == 0", exprtypes.KindBool, true},
		{"numeric string coercion", "'711' == 711", exprtypes.KindBool, true},
		{"nonnumeric string equality", "'no number' == 0", exprtypes.KindBool, false},
		{"NaN less", "'no number' < 0", exprtypes.KindBool, false},
		{"NaN less equal", "'no number' <= 0", exprtypes.KindBool, false},
		{"NaN greater", "'no number' > 0", exprtypes.KindBool, false},
		{"NaN greater equal", "'no number' >= 0", exprtypes.KindBool, false},
		{"contains string", "contains('Hello world', 'llo')", exprtypes.KindBool, true},
		{"contains ignores case", "contains('Hello world', 'LLO')", exprtypes.KindBool, true},
		{"contains absent substring", "contains('Hello world', 'goodbye')", exprtypes.KindBool, false},
		{"contains fromJSON push", `contains(fromJSON('["push", "pull_request"]'), 'push')`, exprtypes.KindBool, true},
		{"contains fromJSON pull request", `contains(fromJSON('["push", "pull_request"]'), 'pull_request')`, exprtypes.KindBool, true},
		{"contains fromJSON other event", `contains(fromJSON('["push", "pull_request"]'), 'issues')`, exprtypes.KindBool, false},
		{"array membership is not substring matching", `contains(fromJSON('["push", "pull_request"]'), 'pull')`, exprtypes.KindBool, false},
		{"contains array ignores case", `contains(fromJSON('["push"]'), 'PUSH')`, exprtypes.KindBool, true},
		{"mixed case function names", `CoNtAiNs(fRoMjSoN('["push"]'), 'push')`, exprtypes.KindBool, true},
		{"number string coercion", "endsWith(711, '11')", exprtypes.KindBool, true},
		{"startsWith", "startsWith('Hello world', 'He')", exprtypes.KindBool, true},
		{"startsWith ignores case", "startsWith('Hello world', 'he')", exprtypes.KindBool, true},
		{"endsWith", "endsWith('Hello world', 'ld')", exprtypes.KindBool, true},
		{"endsWith ignores case", "endsWith('Hello world', 'LD')", exprtypes.KindBool, true},
		{"format", "format('Hello {0} {1} {2}', 'Mona', 'the', 'Octocat')", exprtypes.KindString, "Hello Mona the Octocat"},
		{"format escaped braces", "format('{{Hello {0} {1} {2}!}}', 'Mona', 'the', 'Octocat')", exprtypes.KindString, "{Hello Mona the Octocat!}"},
		{"join separator", `join(fromJSON('["bug", "help wanted"]'), ', ')`, exprtypes.KindString, "bug, help wanted"},
		{"join default separator", `join(fromJSON('["bug", "help wanted"]'))`, exprtypes.KindString, "bug,help wanted"},
		{"join string", "join('bug', ', ')", exprtypes.KindString, "bug"},
		{"toJSON scalar", "toJSON(false)", exprtypes.KindString, "false"},
		{"fromJSON boolean", "fromJSON('true')", exprtypes.KindBool, true},
		{"fromJSON integer", "fromJSON('3')", exprtypes.KindNumber, float64(3)},
		{"fromJSON numerical comparison", "fromJSON('10') > 3", exprtypes.KindBool, true},
		{"fromJSON string", `fromJSON('"hello"')`, exprtypes.KindString, "hello"},
		{"fromJSON null", "fromJSON('null')", exprtypes.KindNull, nil},
		{"fromJSON array", `fromJSON('["push", "pull_request"]')`, exprtypes.KindArray, []any{"push", "pull_request"}},
		{"fromJSON object", `fromJSON('{"status":"success"}')`, exprtypes.KindObject, map[string]any{"status": "success"}},
		{"case first match", "case(true, 'first', true, 'second', 'default')", exprtypes.KindString, "first"},
		{"case default", "case(false, 'first', false, 'second', 'default')", exprtypes.KindString, "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node, ty := checkGitHubReference(t, tc.input)
			if ty.Kind() != tc.kind {
				t.Fatalf("type = %s, want %s", ty, tc.kind)
			}
			got, known := expreval.ConditionConstantValue(node)
			if !known {
				t.Fatal("documented constant was not evaluated")
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("value mismatch (-want +got):\n%s", diff)
			}
			runnerreference.CheckValue(t, tc.input, nil, tc.want)
		})
	}
}

func TestGitHubReferenceCoercions(t *testing.T) {
	t.Parallel()
	// Supported coercions infer bool without type errors, independently of warning selection.
	for _, tc := range []struct {
		input string
		want  bool
		kind  exprtypes.Kind
		kinds []findingKind
	}{
		{"fromJSON('[]') == 0", false, exprtypes.KindBool, nil},
		{"fromJSON('{}') >= 0", false, exprtypes.KindBool, nil},
		{"contains(null, '')", true, exprtypes.KindBool, nil},
		{"startsWith(true, 'TR')", true, exprtypes.KindBool, nil},
		{"true > false", true, exprtypes.KindBool, nil},
		{"null <= 0", true, exprtypes.KindBool, nil},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, ty := checkGitHubReference(t, tc.input, tc.kinds...)
			got, known := expreval.ConditionConstantValue(node)
			if ty.Kind() != tc.kind || !known || got != tc.want {
				t.Fatalf("type=%s value=%v known=%t, want type=%s value=%t", ty, got, known, tc.kind, tc.want)
			}
			runnerreference.CheckValue(t, tc.input, nil, tc.want)
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
			got, known := expreval.ConditionConstantValue(node)
			if !known || exprvalues.ExpressionTruthy(got) != tc.want {
				t.Fatalf("value=%v known=%t, want truthy=%t", got, known, tc.want)
			}
		})
	}
	if _, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(`"double quoted"}}`)); err == nil {
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
			if got, known := expreval.ConditionConstantValue(node); known {
				t.Fatalf("invented context value: %v", got)
			}
			context := map[string]any{"github": map[string]any{tc.property: tc.value}}
			got, known := expreval.ExpressionValue(node, func(name string) (any, bool) {
				v, ok := context[name]
				return v, ok
			})
			if !known || got != tc.want {
				t.Fatalf("value=%v known=%t, want %v", got, known, tc.want)
			}
			runnerreference.CheckValue(t, tc.input, context, tc.want)
		})
	}
}

func TestGitHubReferenceRuntimeExpressions(t *testing.T) {
	t.Parallel()
	// These expressions are valid, but the constant evaluator must not invent
	// filesystem contents, execution status, object identity, or JSON formatting.
	for _, tc := range []struct {
		input string
		kind  exprtypes.Kind
	}{
		{"contains(github.event.issue.labels.*.name, 'bug')", exprtypes.KindBool},
		{"join(github.event.issue.labels.*.name, ', ')", exprtypes.KindString},
		{"toJSON(job)", exprtypes.KindString},
		{"hashFiles('**/package-lock.json')", exprtypes.KindString},
		{"hashFiles('/src/*.js')", exprtypes.KindString},
		{"hashFiles('/lib/**/*.rb')", exprtypes.KindString},
		{"hashFiles('**/package-lock.json', '**/Gemfile.lock')", exprtypes.KindString},
		{"hashFiles('/lib/**/*.rb', '!/lib/foo/*.rb')", exprtypes.KindString},
		{"success()", exprtypes.KindBool},
		{"always()", exprtypes.KindBool},
		{"cancelled()", exprtypes.KindBool},
		{"failure()", exprtypes.KindBool},
		{"failure() && steps.demo.conclusion == 'failure'", exprtypes.KindBool},
		{"job.status", exprtypes.KindString},
		{"steps.demo.outputs.number", exprtypes.KindString},
		{"fromJSON(steps.demo.outputs.number) > 3", exprtypes.KindBool},
		{"fromJSON('{}') == fromJSON('{}')", exprtypes.KindBool},
		{"fromJSON('[]') == fromJSON('[]')", exprtypes.KindBool},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			node, ty := checkGitHubReference(t, tc.input)
			if ty.Kind() != tc.kind {
				t.Fatalf("type = %s, want %s", ty, tc.kind)
			}
			if got, known := expreval.ConditionConstantValue(node); known {
				t.Fatalf("invented runtime result: %v", got)
			}
		})
	}
}

func TestGitHubReferenceObjectFilters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		elem  exprtypes.Type
	}{
		{`fromJSON('[{"name":"apple","quantity":1},{"name":"orange","quantity":2},{"name":"pear","quantity":1}]').*.name`, exprtypes.StringType()},
		{`fromJSON('{"scallions":{"ediblePortions":["roots","stalks"]},"beets":{"ediblePortions":["roots","stems","leaves"]},"artichokes":{"ediblePortions":["hearts","stems","leaves"]}}').*.ediblePortions`, exprtypes.AnyType()},
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

func checkGitHubReference(t *testing.T, input string, wantDiagnostics ...findingKind) (exprsyntax.Node, exprtypes.Type) {
	t.Helper()
	node, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(input + "}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(contextspec.DefaultExpressionScope(), nil)
	checker.SetAvailability(contextspec.WorkflowExpressionScope("jobs.<job_id>.steps.if"))
	checker.SetContextType("steps", exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"demo": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
			"conclusion": exprtypes.StringType(), "outputs": exprtypes.NewMapObjectType(exprtypes.StringType()).Type(),
		}).Type(),
	}).Type())
	ty, diagnostics := checker.Check(expreval.EvaluateSubexpressions(node))
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
