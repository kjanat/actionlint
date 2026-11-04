package expression

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Examples and semantic boundaries from GitHub's expression reference, checked
// on 2026-11-04. Tests use local inputs and never fetch the page:
// https://docs.github.com/en/actions/reference/workflows-and-actions/expressions
func TestGitHubReferenceValues(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		kind        Kind
		want        any
	}{
		{"literal null", "null", KindNull, nil},
		{"literal boolean", "false", KindBool, false},
		{"literal integer", "711", KindNumber, float64(711)},
		{"literal decimal", "-9.2", KindNumber, -9.2},
		{"literal hexadecimal", "0xff", KindNumber, float64(255)},
		{"literal exponent", "-2.99e-2", KindNumber, -0.0299},
		{"literal escaped quote", "'It''s open source!'", KindString, "It's open source!"},
		{"grouping and boolean operators", "!(false || true && false)", KindBool, true},
		{"array index", `fromJSON('["first", "second"]')[1]`, KindString, "second"},
		{"property dereference", `fromJSON('{"status":"success"}').status`, KindString, "success"},
		{"property index", `fromJSON('{"status":"success"}')['status']`, KindString, "success"},
		{"less", "1 < 2", KindBool, true},
		{"less equal", "2 <= 2", KindBool, true},
		{"greater", "2 > 1", KindBool, true},
		{"greater equal", "2 >= 2", KindBool, true},
		{"not equal", "1 != 2", KindBool, true},
		{"case insensitive equality", "'Hello' == 'HELLO'", KindBool, true},
		{"null numeric coercion", "null == 0", KindBool, true},
		{"true numeric coercion", "true == 1", KindBool, true},
		{"false numeric coercion", "false == 0", KindBool, true},
		{"empty string numeric coercion", "'' == 0", KindBool, true},
		{"numeric string coercion", "'711' == 711", KindBool, true},
		{"nonnumeric string equality", "'no number' == 0", KindBool, false},
		{"NaN less", "'no number' < 0", KindBool, false},
		{"NaN less equal", "'no number' <= 0", KindBool, false},
		{"NaN greater", "'no number' > 0", KindBool, false},
		{"NaN greater equal", "'no number' >= 0", KindBool, false},
		{"contains string", "contains('Hello world', 'llo')", KindBool, true},
		{"contains ignores case", "contains('Hello world', 'LLO')", KindBool, true},
		{"contains absent substring", "contains('Hello world', 'goodbye')", KindBool, false},
		{"contains fromJSON push", `contains(fromJSON('["push", "pull_request"]'), 'push')`, KindBool, true},
		{"contains fromJSON pull request", `contains(fromJSON('["push", "pull_request"]'), 'pull_request')`, KindBool, true},
		{"contains fromJSON other event", `contains(fromJSON('["push", "pull_request"]'), 'issues')`, KindBool, false},
		{"array membership is not substring matching", `contains(fromJSON('["push", "pull_request"]'), 'pull')`, KindBool, false},
		{"contains array ignores case", `contains(fromJSON('["push"]'), 'PUSH')`, KindBool, true},
		{"mixed case function names", `CoNtAiNs(fRoMjSoN('["push"]'), 'push')`, KindBool, true},
		{"number string coercion", "endsWith(711, '11')", KindBool, true},
		{"startsWith", "startsWith('Hello world', 'He')", KindBool, true},
		{"startsWith ignores case", "startsWith('Hello world', 'he')", KindBool, true},
		{"endsWith", "endsWith('Hello world', 'ld')", KindBool, true},
		{"endsWith ignores case", "endsWith('Hello world', 'LD')", KindBool, true},
		{"format", "format('Hello {0} {1} {2}', 'Mona', 'the', 'Octocat')", KindString, "Hello Mona the Octocat"},
		{"format escaped braces", "format('{{Hello {0} {1} {2}!}}', 'Mona', 'the', 'Octocat')", KindString, "{Hello Mona the Octocat!}"},
		{"join separator", `join(fromJSON('["bug", "help wanted"]'), ', ')`, KindString, "bug, help wanted"},
		{"join default separator", `join(fromJSON('["bug", "help wanted"]'))`, KindString, "bug,help wanted"},
		{"join string", "join('bug', ', ')", KindString, "bug"},
		{"toJSON scalar", "toJSON(false)", KindString, "false"},
		{"fromJSON boolean", "fromJSON('true')", KindBool, true},
		{"fromJSON integer", "fromJSON('3')", KindNumber, float64(3)},
		{"fromJSON numerical comparison", "fromJSON('10') > 3", KindBool, true},
		{"fromJSON string", `fromJSON('"hello"')`, KindString, "hello"},
		{"fromJSON null", "fromJSON('null')", KindNull, nil},
		{"fromJSON array", `fromJSON('["push", "pull_request"]')`, KindArray, []any{"push", "pull_request"}},
		{"fromJSON object", `fromJSON('{"status":"success"}')`, KindObject, map[string]any{"status": "success"}},
		{"case first match", "case(true, 'first', true, 'second', 'default')", KindString, "first"},
		{"case default", "case(false, 'first', false, 'second', 'default')", KindString, "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, ty := checkGitHubReference(t, tc.input)
			if ty.Kind() != tc.kind {
				t.Fatalf("type = %s, want %s", ty, tc.kind)
			}
			got, known := ConditionConstantValue(node)
			if !known {
				t.Fatal("documented constant was not evaluated")
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("value mismatch (-want +got):\n%s", diff)
			}
			checkGitHubReferenceLive(t, tc.input, nil, tc.want)
		})
	}
}

func TestGitHubReferenceStrictCoercionDiagnostics(t *testing.T) {
	// GitHub defines these conversions, but actionlint deliberately reports them
	// as likely mistakes. Test runtime values separately from the lint policy.
	// See docs/checks.md#check-comparison-types and Type.Assignable.
	for _, tc := range []struct {
		input string
		want  bool
		kind  Kind
		kinds []findingKind
	}{
		{"fromJSON('[]') == 0", false, KindBool, []findingKind{operandType}},
		{"fromJSON('{}') >= 0", false, KindBool, []findingKind{operandType}},
		{"contains(null, '')", true, KindAny, []findingKind{argumentType, argumentType}},
		{"startsWith(true, 'TR')", true, KindAny, []findingKind{argumentType}},
		{"true > false", true, KindBool, []findingKind{operandType}},
		{"null <= 0", true, KindBool, []findingKind{operandType}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			node, ty := checkGitHubReference(t, tc.input, tc.kinds...)
			got, known := ConditionConstantValue(node)
			if ty.Kind() != tc.kind || !known || got != tc.want {
				t.Fatalf("type=%s value=%v known=%t, want type=%s value=%t", ty, got, known, tc.kind, tc.want)
			}
			checkGitHubReferenceLive(t, tc.input, nil, tc.want)
		})
	}
}

func TestGitHubReferenceTruthiness(t *testing.T) {
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
			node, _ := checkGitHubReference(t, tc.input)
			got, known := ConditionConstantValue(node)
			if !known || ExpressionTruthy(got) != tc.want {
				t.Fatalf("value=%v known=%t, want truthy=%t", got, known, tc.want)
			}
		})
	}
	if _, err := NewParser().Parse(NewLexer(`"double quoted"}}`)); err == nil {
		t.Fatal("double-quoted expression string was accepted")
	}
}

func TestGitHubReferenceContextScenarios(t *testing.T) {
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
			node, _ := checkGitHubReference(t, tc.input)
			if got, known := ConditionConstantValue(node); known {
				t.Fatalf("invented context value: %v", got)
			}
			context := map[string]any{"github": map[string]any{tc.property: tc.value}}
			got, known := ExpressionValue(node, func(name string) (any, bool) {
				v, ok := context[name]
				return v, ok
			})
			if !known || got != tc.want {
				t.Fatalf("value=%v known=%t, want %v", got, known, tc.want)
			}
			checkGitHubReferenceLive(t, tc.input, context, tc.want)
		})
	}
}

func TestGitHubReferenceRuntimeExpressions(t *testing.T) {
	// These expressions are valid, but the constant evaluator must not invent
	// filesystem contents, execution status, object identity, or JSON formatting.
	for _, tc := range []struct {
		input string
		kind  Kind
	}{
		{"contains(github.event.issue.labels.*.name, 'bug')", KindBool},
		{"join(github.event.issue.labels.*.name, ', ')", KindString},
		{"toJSON(job)", KindString},
		{"hashFiles('**/package-lock.json')", KindString},
		{"hashFiles('/src/*.js')", KindString},
		{"hashFiles('/lib/**/*.rb')", KindString},
		{"hashFiles('**/package-lock.json', '**/Gemfile.lock')", KindString},
		{"hashFiles('/lib/**/*.rb', '!/lib/foo/*.rb')", KindString},
		{"success()", KindBool},
		{"always()", KindBool},
		{"cancelled()", KindBool},
		{"failure()", KindBool},
		{"failure() && steps.demo.conclusion == 'failure'", KindBool},
		{"job.status", KindString},
		{"steps.demo.outputs.number", KindString},
		{"fromJSON(steps.demo.outputs.number) > 3", KindBool},
		{"fromJSON('{}') == fromJSON('{}')", KindBool},
		{"fromJSON('[]') == fromJSON('[]')", KindBool},
	} {
		t.Run(tc.input, func(t *testing.T) {
			node, ty := checkGitHubReference(t, tc.input)
			if ty.Kind() != tc.kind {
				t.Fatalf("type = %s, want %s", ty, tc.kind)
			}
			if got, known := ConditionConstantValue(node); known {
				t.Fatalf("invented runtime result: %v", got)
			}
		})
	}
}

func TestGitHubReferenceObjectFilters(t *testing.T) {
	for _, tc := range []struct {
		input string
		elem  Type
	}{
		{`fromJSON('[{"name":"apple","quantity":1},{"name":"orange","quantity":2},{"name":"pear","quantity":1}]').*.name`, StringType()},
		{`fromJSON('{"scallions":{"ediblePortions":["roots","stalks"]},"beets":{"ediblePortions":["roots","stems","leaves"]},"artichokes":{"ediblePortions":["hearts","stems","leaves"]}}').*.ediblePortions`, AnyType()},
	} {
		t.Run(tc.input, func(t *testing.T) {
			_, ty := checkGitHubReference(t, tc.input)
			array, ok := ty.Array()
			if !ok || !array.Elem().Equal(tc.elem) {
				t.Fatalf("filter type = %s, want array of %s", ty, tc.elem)
			}
		})
	}
}

func checkGitHubReference(t *testing.T, input string, wantDiagnostics ...findingKind) (Node, Type) {
	t.Helper()
	node, err := NewParser().Parse(NewLexer(input + "}}"))
	if err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(false, nil)
	checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.if")
	checker.UpdateSteps(NewStrictObjectType(map[string]Type{
		"demo": NewStrictObjectType(map[string]Type{
			"conclusion": StringType(), "outputs": NewMapObjectType(StringType()).Type(),
		}).Type(),
	}))
	ty, diagnostics := checker.Check(node)
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
