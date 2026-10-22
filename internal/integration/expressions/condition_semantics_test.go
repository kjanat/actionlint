package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestConditionNormalization(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"null", ""},
		{"~", ""},
		{"", ""},
		{`""`, ""},
		{`"  "`, ""},
		{"${{ '' }}", ""},
		{"${{ ' ' }}", ""},
		{"${{ 'failure()' }}", ""},
		{`"null"`, "always falsy"},
		{"${{ null }}", "always falsy"},
		{"${{ 'false' }}", "always falsy"},
		{"${{ '1 == 2' }}", "always falsy"},
		{"${{ format('') }}", "always falsy"},
		{"${{ format('false') }}", "always truthy"},
		{"${{ fromJSON('false') }}", "always falsy"},
		{"${{ 'fromJSON(''false'')' }}", "always falsy"},
		{"${{ fromJSON('true') }}", "always truthy"},
		{"${{ toJSON(false) }}", "always truthy"},
		{"${{ toJSON(null) }}", "always truthy"},
		{"${{ toJSON('') }}", "always truthy"},
		{"${{ case(true, false, true) }}", "always falsy"},
		{"${{ case(false, false, true) }}", "always truthy"},
		{"${{ case(1, false, true) }}", "predicate must be boolean"},
		{"${{ case(null, false, true) }}", "predicate must be boolean"},
		{"${{ case(false, true, true, false, true) }}", "always falsy"},
		{"${{ case(false, true, false, true, false) }}", "always falsy"},
		{"${{ case(true, true, fromJSON('broken')) }}", "always truthy"},
		{`${{ contains(fromJSON('["x"]'), 'X') }}`, "always truthy"},
		{`${{ contains(fromJSON('["x"]'), 'y') }}`, "always falsy"},
		{`${{ fromJSON('{}') == null }}`, "always falsy"},
		{`${{ null != fromJSON('[]') }}`, "always truthy"},
		{"${{ join('') }}", "always falsy"},
		{"${{ join('x') }}", "always truthy"},
		{"${{ join('x', ':') }}", "always truthy"},
		{`${{ join(fromJSON('[]')) }}`, "always falsy"},
		{`${{ join(fromJSON('["", ""]'), '') }}`, "always falsy"},
		{`${{ join(fromJSON('["", ""]')) }}`, "always truthy"},
		{`${{ join(fromJSON('["a", "b"]'), '-') }}`, "always truthy"},
		{`${{ join(fromJSON('[{}, ""]'), '') }}`, "always truthy"},
		{`${{ join(fromJSON('[[], ""]'), '') }}`, "always truthy"},
		{"false", "always falsy"},
		{"true", "always truthy"},
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
					body += "    if: " + tc.source + `
    steps:
      - run: echo ok
`
				case "snapshot":
					body += `    snapshot:
      image-name: test
      if: ` + tc.source + `
    steps:
      - run: echo ok
`
				case "step":
					body += `    steps:
      - run: echo ok
        if: ` + tc.source + "\n"
				}
				errs := testutil.WorkflowDiagnostics(t, `on: push
jobs:
  test:
`+body, "")
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
	source := `on: push
env: {X: 'false'}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
        name: ${{ 'nope' }}
        env: {Y: "${{ 'nope' }}"}
        if: ${{ env.X }}
`
	if errs := testutil.WorkflowDiagnostics(t, source, ""); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestSuspiciousConditionRuleSelection(t *testing.T) {
	const header = `on:
  issue_comment:
  label:
  deployment:
  workflow_dispatch:
    inputs:
      text: {type: string}
      flag: {type: boolean}
      count: {type: number}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: producer
        run: echo ok
      - run: echo ok
        if: `
	for _, tc := range []struct {
		check, expression string
		warn              bool
	}{
		{"string-conditions", "inputs.text", true},
		{"string-conditions", "steps.producer.outputs.flag", true},
		{"string-conditions", "github.event.comment.body", true},
		{"string-conditions", "inputs.flag", false},
		{"string-conditions", "inputs.text == 'true'", false},
		{"string-conditions", "fromJSON(inputs.text)", false},
		{"string-conditions", "fromJSON(inputs.text).unknown", false},
		{"mixed-type-comparisons", "inputs.text == false", true},
		{"mixed-type-comparisons", "0 >= steps.producer.outputs.flag", true},
		{"mixed-type-comparisons", "inputs['text'] <= 100", true},
		{"mixed-type-comparisons", "inputs.count <= 100", false},
		{"mixed-type-comparisons", "inputs.flag == false", false},
		{"mixed-type-comparisons", "inputs.text == 'false'", false},
		{"case-insensitive-conditions", "github.ref == 'refs/heads/main'", true},
		{"case-insensitive-conditions", "github.head_ref == 'release'", true},
		{"case-insensitive-conditions", "'admin' != github.actor", true},
		{"case-insensitive-conditions", "startsWith(github.ref, 'refs/heads/release')", true},
		{"case-insensitive-conditions", "contains(github.event.label.name, 'approved')", true},
		{"case-insensitive-conditions", "endsWith(github.event.deployment.environment, 'production')", true},
		{"case-insensitive-conditions", "github.event.comment.body == 'hello'", false},
		{"case-insensitive-conditions", "contains(fromJSON('[\"a\",\"b\"]'), github.actor)", false},
	} {
		for _, config := range []string{"", "policy: {}", "policy: null", "lint: {rules: {suspicious: off}}", "lint: {rules: {suspicious: {" + tc.check + ": on}}}"} {
			t.Run(tc.check+"/"+tc.expression+"/"+config, func(t *testing.T) {
				errs := testutil.WorkflowDiagnostics(t, header+"${{ "+tc.expression+" }}\n", config)
				if tc.warn && strings.Contains(config, ": on") {
					want := map[string]string{
						"string-conditions":           "bare string condition",
						"mixed-type-comparisons":      "condition compares a string",
						"case-insensitive-conditions": "ignores case",
					}[tc.check]
					if len(errs) != 1 || errs[0].Rule != tc.check || !strings.Contains(errs[0].Message, want) || strings.Contains(errs[0].Message, "policy") {
						t.Fatal(errs)
					}
				} else if len(errs) != 0 {
					t.Fatal(errs)
				}
			})
		}
	}
}
