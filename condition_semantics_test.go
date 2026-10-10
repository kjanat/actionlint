package actionlint

import (
	"fmt"
	"strings"
	"testing"
)

func TestConditionNormalization(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"null", ""}, {"~", ""}, {"", ""}, {`""`, ""}, {`"  "`, ""},
		{"${{ '' }}", ""}, {"${{ ' ' }}", ""}, {"${{ 'failure()' }}", ""},
		{`"null"`, "always falsy"}, {"${{ null }}", "always falsy"},
		{"${{ 'false' }}", "always falsy"}, {"${{ '1 == 2' }}", "always falsy"},
		{"${{ format('') }}", "always falsy"}, {"${{ format('false') }}", "always truthy"},
		{"${{ fromJSON('false') }}", "always falsy"},
		{"${{ 'fromJSON(''false'')' }}", "always falsy"},
		{"${{ fromJSON('true') }}", "always truthy"},
		{"${{ case(true, false, true) }}", "always falsy"},
		{"${{ case(false, false, true) }}", "always truthy"},
		{"${{ case(1, false, true) }}", "always falsy"},
		{"${{ case(null, false, true) }}", "always truthy"},
		{"${{ case(false, true, true, false, true) }}", "always falsy"},
		{"${{ case(false, true, false, true, false) }}", "always falsy"},
		{"${{ case(true, true, fromJSON('broken')) }}", "broken JSON"},
		{`${{ contains(fromJSON('["x"]'), 'X') }}`, "always truthy"},
		{`${{ contains(fromJSON('["x"]'), 'y') }}`, "always falsy"},
		{`${{ fromJSON('{}') == null }}`, "always falsy"},
		{`${{ null != fromJSON('[]') }}`, "always truthy"},
		{"${{ join('') }}", "always falsy"}, {"${{ join('x') }}", "always truthy"},
		{"${{ join('x', ':') }}", "always truthy"},
		{`${{ join(fromJSON('[]')) }}`, "always falsy"},
		{`${{ join(fromJSON('["", ""]'), '') }}`, "always falsy"},
		{`${{ join(fromJSON('["", ""]')) }}`, "always truthy"},
		{`${{ join(fromJSON('["a", "b"]'), '-') }}`, "always truthy"},
		{"false", "always falsy"}, {"true", "always truthy"},
		{"${{ 'nope' }}", "undefined variable"},
		{"${{ '${{ false }}' }}", "unexpected"},
		{"${{ '}}' }}", "unexpected"},
		{"${{ 'github.ref }} ignored' }}", "unexpected"},
		{"${{ true || failure() }}", ""},
	} {
		for _, location := range []string{"step", "job", "snapshot"} {
			t.Run(location+"/"+tc.source, func(t *testing.T) {
				body := "    runs-on: ubuntu-latest\n"
				switch location {
				case "job":
					body += "    if: " + tc.source + "\n    steps:\n      - run: echo ok\n"
				case "snapshot":
					body += "    snapshot:\n      image-name: test\n      if: " + tc.source + "\n    steps:\n      - run: echo ok\n"
				case "step":
					body += "    steps:\n      - run: echo ok\n        if: " + tc.source + "\n"
				}
				errs := lintCachePolicy(t, "on: push\njobs:\n  test:\n"+body, "")
				if tc.want == "" {
					if len(errs) != 0 {
						t.Fatal(errs)
					}
				} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
					t.Fatalf("want %q, got %v", tc.want, errs)
				}
				if tc.want == "always falsy" && strings.Contains(errs[0].Message, "remove") {
					t.Fatal("removing a false condition changes execution")
				}
			})
		}
	}
}

func TestConditionComputedValuesStayData(t *testing.T) {
	source := "on: push\nenv: {X: 'false'}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        name: ${{ 'nope' }}\n        env: {Y: \"${{ 'nope' }}\"}\n        if: ${{ env.X }}\n"
	if errs := lintCachePolicy(t, source, ""); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestConditionConstantOutcomes(t *testing.T) {
	for _, tc := range []struct {
		expression string
		truthy     bool
	}{
		{"'0'", true}, {"'0' == false", true}, {"'' == 0", true},
		{"0 == '0'", true}, {"'' == '0'", false}, {"'0.0' == 0", true},
		{"'0x0' == 0", true}, {"'0x80000000' <= 100", true},
		{"'false' == false", false}, {"1 == 2", false},
		{"false || (true && true)", true}, {"0 || 1", true},
		{"format('')", false}, {"format('false')", true},
		{"contains(format('{0} {1}', 'foo', 'bar'), 'O B')", true},
		{"fromJSON('false')", false}, {"fromJSON('[]')", true},
		{`contains(fromJSON('["x"]'), 'X')`, true},
		{`contains(fromJSON('["x"]'), 'y')`, false},
		{`contains(fromJSON('[]'), null)`, false},
		{`contains(fromJSON('[false]'), 0)`, true},
		{`contains(fromJSON('[null]'), '')`, true},
		{`contains(fromJSON('[{}, null]'), null)`, true},
		{`contains(fromJSON('[{}]'), null)`, false},
		{`fromJSON('{}') == null`, false},
		{`null == fromJSON('[]')`, false},
		{`fromJSON('[]') != null`, true},
		{`null != fromJSON('{}')`, true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr, err := NewExprParser().Parse(NewExprLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			value, known := conditionConstantValue(expr)
			if !known || expressionTruthy(value) != tc.truthy {
				t.Fatalf("value=%v known=%v", value, known)
			}
		})
	}
}

func TestConditionConstantCase(t *testing.T) {
	for _, tc := range []struct {
		expression string
		value      any
		known      bool
	}{
		{"case(true, false, true)", false, true},
		{"case(false, false, true)", true, true},
		{"case(false, 'first', true, 'second', 'fallback')", "second", true},
		{"case(false, 'first', false, 'second', 'fallback')", "fallback", true},
		{"case(true, 'first', true, 'second', 'fallback')", "first", true},
		{"CASE(true, null, 1)", nil, true},
		{"case(true, 0, 1)", float64(0), true},
		{"case(true, '', 'fallback')", "", true},
		{"case(true, false, inputs.fallback)", false, true},
		{"case(false, inputs.skipped, true)", true, true},
		{"case(true, false, inputs.predicate, inputs.value, inputs.fallback)", false, true},
		{"case(false, fromJSON('broken'), true)", true, true},
		{"case(true, case(false, false, true), false)", true, true},
		{"case(inputs.predicate, false, true)", nil, false},
		{"case(true, inputs.value, false)", nil, false},
		{"case(false, true, inputs.fallback)", nil, false},
		{"case(1, true, false)", true, true},
		{"case('false', true, false)", true, true},
		{"case(null, true, false)", false, true},
		{"case(false, true, 1, false, true)", false, true},
		{"case(0, true, false)", false, true},
		{"case('', true, false)", false, true},
		{"case(fromJSON('[]'), true, false)", true, true},
		{"case()", nil, false},
		{"case(true)", nil, false},
		{"case(true, false)", nil, false},
		{"case(true, false, true, false)", nil, false},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr := parseAssignedExpression("${{ " + tc.expression + " }}")
			if expr == nil {
				t.Fatal("invalid fixture")
			}
			value, known := conditionConstantValue(expr)
			if known != tc.known || value != tc.value {
				t.Fatalf("value=%v known=%v, want value=%v known=%v", value, known, tc.value, tc.known)
			}
		})
	}
}

func TestConditionSerializationRemainsUnknown(t *testing.T) {
	// The engines disagree on signed-zero serialization; strict JSON rejects
	// non-finite round trips. Do not invent a single folded outcome.
	for _, expression := range []string{
		"toJSON(-0)", "format('{0}', -0)", "fromJSON(toJSON(fromJSON('1e309')))",
		`fromJSON('{"approved":false,"APPROVED":true}')`,
		`fromJSON('{}') == fromJSON('{}')`,
		`contains(fromJSON('[{}]'), fromJSON('{}'))`,
	} {
		expr := parseAssignedExpression("${{ " + expression + " }}")
		if expr == nil {
			t.Fatalf("invalid fixture %s", expression)
		}
		if v, known := conditionConstantValue(expr); known {
			t.Fatalf("invented outcome for %s: %v", expression, v)
		}
	}
}

func TestConditionPolicies(t *testing.T) {
	const header = "on:\n  workflow_dispatch:\n    inputs:\n      text: {type: string}\n      flag: {type: boolean}\n      count: {type: number}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - id: producer\n        run: echo ok\n      - run: echo ok\n        if: "
	for _, tc := range []struct {
		policy, expression string
		warn               bool
	}{
		{"string-conditions", "inputs.text", true},
		{"string-conditions", "steps.producer.outputs.flag", true},
		{"string-conditions", "github.event.comment.body", true},
		{"string-conditions", "inputs.flag", false},
		{"string-conditions", "inputs.text == 'true'", false},
		{"string-conditions", "fromJSON(inputs.text)", false},
		{"string-conditions", "github.event.unknown", false},
		{"mixed-type-comparisons", "inputs.text == false", true},
		{"mixed-type-comparisons", "0 >= steps.producer.outputs.flag", true},
		{"mixed-type-comparisons", "inputs['text'] <= 100", true},
		{"mixed-type-comparisons", "inputs.count <= 100", false},
		{"mixed-type-comparisons", "inputs.flag == false", false},
		{"mixed-type-comparisons", "inputs.text == 'false'", false},
		{"case-insensitive-conditions", "github.head_ref == 'release'", true},
		{"case-insensitive-conditions", "'admin' != github.actor", true},
		{"case-insensitive-conditions", "startsWith(github.ref, 'refs/heads/release')", true},
		{"case-insensitive-conditions", "contains(github.event.label.name, 'approved')", true},
		{"case-insensitive-conditions", "endsWith(github.event.deployment.environment, 'production')", true},
		{"case-insensitive-conditions", "github.event.comment.body == 'hello'", false},
		{"case-insensitive-conditions", "contains(fromJSON('[\"a\",\"b\"]'), github.actor)", false},
	} {
		for _, value := range []string{"true", "false", "null", "omitted"} {
			t.Run(tc.policy+"/"+tc.expression+"/"+value, func(t *testing.T) {
				config := ""
				if value != "omitted" {
					config = fmt.Sprintf("policy: {%s: %s}", tc.policy, value)
				}
				errs := lintCachePolicy(t, header+"${{ "+tc.expression+" }}\n", config)
				want := tc.warn && value == "true"
				if want {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, "policy: "+tc.policy) {
						t.Fatal(errs)
					}
				} else if len(errs) != 0 {
					t.Fatal(errs)
				}
			})
		}
	}
}
