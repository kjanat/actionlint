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
		{"false", "always falsy"}, {"true", "always truthy"},
		{"${{ 'nope' }}", "undefined variable"},
		{"${{ '${{ false }}' }}", "unexpected"},
		{"${{ '}}' }}", "unexpected"},
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

func TestConditionSerializationRemainsUnknown(t *testing.T) {
	// The engines disagree on signed-zero serialization; strict JSON rejects
	// non-finite round trips. Do not invent a single folded outcome.
	for _, expression := range []string{
		"toJSON(-0)", "format('{0}', -0)", "fromJSON(toJSON(fromJSON('1e309')))",
		`fromJSON('{"approved":false,"APPROVED":true}')`,
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
