package actionlint

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// These profiles cover every expression-bearing workflow key in the runner
// schema and documentation table, including inherited mapping contexts.
// Runner: 759385a3510197a58b5c08dc1f373b74b9f4643b
// Languageservices: 4043eda158e16579cc5fb1b0b07a4bce2a76f0b5
func TestSchemaAuditContextProfiles(t *testing.T) {
	profiles := []struct {
		keys     string
		contexts string
		funcs    string
	}{
		{"run-name concurrency on.workflow_call.inputs.<inputs_id>.default", "github inputs vars", ""},
		{"env", "github inputs secrets vars", ""},
		{"on.workflow_call.outputs.<output_id>.value", "github inputs jobs vars", ""},
		{"jobs.<job_id>.if", "github inputs needs vars", "always cancelled failure success"},
		{"jobs.<job_id>.strategy", "github inputs needs vars", ""},
		{"jobs.<job_id>.cancel-timeout-minutes jobs.<job_id>.snapshot jobs.<job_id>.concurrency jobs.<job_id>.container jobs.<job_id>.container.image jobs.<job_id>.continue-on-error jobs.<job_id>.environment jobs.<job_id>.name jobs.<job_id>.runs-on jobs.<job_id>.services jobs.<job_id>.timeout-minutes jobs.<job_id>.with.<with_id>", "github inputs matrix needs strategy vars", ""},
		{"jobs.<job_id>.env jobs.<job_id>.secrets.<secrets_id>", "github inputs matrix needs secrets strategy vars", ""},
		{"jobs.<job_id>.defaults.run", "env github inputs matrix needs strategy vars", ""},
		{"jobs.<job_id>.container.credentials jobs.<job_id>.services.<service_id>.credentials", "env github inputs matrix needs secrets strategy vars", ""},
		{"jobs.<job_id>.container.env.<env_id> jobs.<job_id>.services.<service_id>.env.<env_id>", "env github inputs job matrix needs runner secrets strategy vars", ""},
		{"jobs.<job_id>.environment.url", "env github inputs job matrix needs runner steps strategy vars", ""},
		{"jobs.<job_id>.outputs.<output_id>", "env github inputs job matrix needs runner secrets steps strategy vars", ""},
		{"jobs.<job_id>.snapshot.if jobs.<job_id>.steps.if", "env github inputs job matrix needs runner steps strategy vars", "always cancelled failure hashfiles success"},
		{"jobs.<job_id>.steps.continue-on-error jobs.<job_id>.steps.env jobs.<job_id>.steps.name jobs.<job_id>.steps.run jobs.<job_id>.steps.timeout-minutes jobs.<job_id>.steps.with jobs.<job_id>.steps.working-directory", "env github inputs job matrix needs runner secrets steps strategy vars", "hashfiles"},
	}
	covered := map[string]bool{}
	for _, profile := range profiles {
		for key := range strings.FieldsSeq(profile.keys) {
			covered[key] = true
			t.Run(key, func(t *testing.T) {
				contexts, functions := WorkflowKeyAvailability(key)
				if !slices.Equal(contexts, strings.Fields(profile.contexts)) || !slices.Equal(functions, strings.Fields(profile.funcs)) {
					t.Fatalf("contexts %v; functions %v", contexts, functions)
				}
				if !slices.Contains(allWorkflowKeys, key) {
					t.Fatal("missing from generated workflow key inventory")
				}
				for context := range BuiltinGlobalVariableTypes {
					checker := NewExprSemanticsChecker(false, nil)
					checker.SetWorkflowKeyAvailability(key)
					expr, err := NewExprParser().Parse(NewExprLexer("toJSON(" + context + ")}}"))
					if err != nil {
						t.Fatal(err)
					}
					_, errs := checker.Check(expr)
					if (len(errs) == 0) != slices.Contains(contexts, context) {
						t.Fatalf("context %s: %v", context, errs)
					}
				}
			})
		}
	}
	for _, key := range allWorkflowKeys {
		if !covered[key] {
			t.Errorf("workflow key %q has no authoritative audit profile", key)
		}
	}
}

func TestSchemaAuditFunctionContracts(t *testing.T) {
	for _, tc := range []struct {
		key, expression string
		valid           bool
	}{
		{"jobs.<job_id>.if", "success()", true},
		{"jobs.<job_id>.if", "success('build', 'test')", true},
		{"jobs.<job_id>.if", "failure('build')", true},
		{"jobs.<job_id>.steps.if", "success('build')", false},
		{"jobs.<job_id>.snapshot.if", "failure('build')", false},
		{"jobs.<job_id>.snapshot.if", "always() && hashFiles('**') != ''", true},
		{"jobs.<job_id>.steps.run", "join('hello')", true},
		{"jobs.<job_id>.steps.run", "join('hello', ',')", true},
		{"jobs.<job_id>.steps.run", "format('hello')", true},
		{"jobs.<job_id>.steps.run", "format('{{hello}}')", true},
		{"jobs.<job_id>.steps.run", "format('{0}')", false},
		{"jobs.<job_id>.steps.run", "hashFiles(" + strings.Repeat("'x',", 254) + "'x')", true},
		{"jobs.<job_id>.steps.run", "hashFiles(" + strings.Repeat("'x',", 255) + "'x')", false},
		{"jobs.<job_id>.steps.run", "case(" + strings.Repeat("true,'x',", 127) + "'y')", true},
		{"jobs.<job_id>.steps.run", "case(" + strings.Repeat("true,'x',", 128) + "'y')", false},
	} {
		t.Run(tc.key+"/"+tc.expression, func(t *testing.T) {
			expr, err := NewExprParser().Parse(NewExprLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewExprSemanticsChecker(false, nil)
			checker.SetWorkflowKeyAvailability(tc.key)
			_, errs := checker.Check(expr)
			if (len(errs) == 0) != tc.valid {
				t.Fatalf("valid=%v, diagnostics=%v", tc.valid, errs)
			}
		})
	}
}

func TestSchemaAuditScalarDecoding(t *testing.T) {
	for _, spelling := range []string{"true", "True", "TRUE"} {
		t.Run(spelling, func(t *testing.T) {
			workflow, errs := Parse([]byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    continue-on-error: " + spelling + "\n    steps:\n      - run: echo test\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			if value := workflow.Jobs["build"].ContinueOnError; value == nil || !value.Value {
				t.Fatalf("decoded true spelling as %#v", value)
			}
		})
	}
	for _, tc := range []struct {
		spelling string
		value    float64
	}{{"0x1e", 30}, {"0o36", 30}, {"030", 30}, {"3e1", 30}} {
		t.Run(tc.spelling, func(t *testing.T) {
			workflow, errs := Parse([]byte(fmt.Sprintf("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    timeout-minutes: %s\n    steps:\n      - run: echo test\n", tc.spelling)))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			if value := workflow.Jobs["build"].TimeoutMinutes; value == nil || value.Value != tc.value {
				t.Fatalf("wanted %v, got %#v", tc.value, value)
			}
		})
	}
}

func TestSchemaAuditRequiredFlagsAreStatic(t *testing.T) {
	for _, event := range []string{
		"workflow_dispatch:\n    inputs:\n      value:\n        type: string\n        required: ${{ true }}",
		"workflow_call:\n    inputs:\n      value:\n        type: string\n        required: ${{ true }}",
		"workflow_call:\n    secrets:\n      value:\n        required: ${{ true }}",
	} {
		t.Run(event, func(t *testing.T) {
			_, errs := Parse([]byte("on:\n  " + event + "\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo test\n"))
			if len(errs) == 0 {
				t.Fatal("accepted expression in a static boolean definition")
			}
		})
	}
}

func TestSchemaAuditExpressionVisitorCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, job, step, want string
	}{
		{"snapshot step output", "snapshot:\n      image-name: image\n      if: steps.build.outputs.ready != '' && success()", "id: build\n        run: echo test", ""},
		{"snapshot missing step", "snapshot:\n      image-name: image\n      if: steps.missing.outputs.ready != ''", "run: echo test", `property "missing"`},
		{"snapshot image invalid context", "snapshot:\n      image-name: ${{ env.IMAGE }}", "run: echo test", `context "env" is not allowed`},
		{"snapshot version invalid context", "snapshot:\n      image-name: image\n      version: ${{ secrets.VERSION }}", "run: echo test", `context "secrets" is not allowed`},
		{"snapshot condition secret", "snapshot:\n      image-name: image\n      if: secrets.READY != ''", "run: echo test", `context "secrets" is not allowed`},
		{"background bool", "", "run: echo test\n        background: ${{ github.event_name == 'push' }}", ""},
		{"background wrong type", "", "run: echo test\n        background: ${{ 1 }}", "type of expression must be bool"},
		{"background malformed expression", "", "run: echo test\n        background: ${{ broken( }}", "unexpected"},
		{"job status arguments", "if: success('prepare')", "run: echo test", ""},
		{"step status arguments", "", "run: echo test\n        if: success('prepare')", "number of arguments is wrong"},
		{"static shell literal expression", "", "run: echo test\n        shell: ${{ 'bash' }}", ""},
		{"static shell function expression", "", "run: echo test\n        shell: ${{ format('bash') }}", "expressions are not allowed"},
		{"static shell interpolation", "", "run: echo test\n        shell: prefix-${{ 'bash' }}", "expressions are not allowed"},
		{"strategy object matrix inference", `strategy: ${{ fromJSON('{"matrix":{"os":["ubuntu-latest"]},"fail-fast":false}') }}`, "run: echo ${{ matrix.os }}", ""},
		{"strategy boolean property", `strategy: ${{ fromJSON('{"fail-fast":1}') }}`, "run: echo test", "strategy.fail-fast must be bool"},
		{"strategy unknown property", `strategy: ${{ fromJSON('{"failfast":false}') }}`, "run: echo test", "unknown property"},
		{"matrix axis expression inference", `strategy: {matrix: "${{ fromJSON('{\"os\":[\"ubuntu-latest\"]}') }}"}`, "run: echo ${{ matrix.os }}", ""},
		{"defaults object", "defaults:\n      run: ${{ fromJSON('{\"shell\":\"bash\",\"working-directory\":\"src\"}') }}", "run: echo test", ""},
		{"defaults unknown key", "defaults:\n      run: ${{ fromJSON('{\"working-dir\":\"src\"}') }}", "run: echo test", "unknown property"},
		{"container object", `container: ${{ fromJSON('{"image":"node:22","env":{"NUMBER":1},"ports":[80]}') }}`, "run: echo test", ""},
		{"container bad ports", `container: ${{ fromJSON('{"image":"node:22","ports":"80"}') }}`, "run: echo test", "container.ports must be array"},
		{"container expression ports", "container:\n      image: node:22\n      ports: ${{ fromJSON('[80]') }}", "run: echo test", ""},
		{"container expression volumes", "container:\n      image: node:22\n      volumes: ${{ fromJSON('[\"/data\"]') }}", "run: echo test", ""},
		{"environment object", `environment: ${{ fromJSON('{"name":"staging","deployment":false}') }}`, "run: echo test", ""},
		{"environment missing name", `environment: ${{ fromJSON('{"url":"https://example.com"}') }}`, "run: echo test", "requires property"},
		{"concurrency object", `concurrency: ${{ fromJSON('{"group":"build","cancel-in-progress":true}') }}`, "run: echo test", ""},
		{"concurrency bad boolean", `concurrency: ${{ fromJSON('{"group":"build","cancel-in-progress":1}') }}`, "run: echo test", "concurrency.cancel-in-progress must be bool"},
		{"concurrency bad queue", `concurrency: ${{ fromJSON('{"group":"build","queue":"banana"}') }}`, "run: echo test", "concurrency.queue must be single or max"},
		{"concurrency empty group", `concurrency: ${{ fromJSON('{"group":""}') }}`, "run: echo test", "concurrency.group must be a non-empty string"},
		{"concurrency queue conflict", `concurrency: ${{ fromJSON('{"group":"build","queue":"max","cancel-in-progress":true}') }}`, "run: echo test", "cannot be combined"},
		{"heterogeneous ports", `container: ${{ fromJSON('{"image":"node","ports":[80,{}]}') }}`, "run: echo test", "container.ports[1] must be non-empty string"},
		{"empty known port", `container: ${{ fromJSON('{"image":"node","ports":[""]}') }}`, "run: echo test", "container.ports[0] must be a non-empty string"},
		{"snapshot object", `snapshot: ${{ fromJSON('{"image-name":"custom","version":"1.*"}') }}`, "run: echo test", ""},
		{"snapshot object missing name", `snapshot: ${{ fromJSON('{"version":"1.*"}') }}`, "run: echo test", "requires property"},
		{"with object", "", "uses: actions/checkout@v6\n        with: ${{ fromJSON('{\"fetch-depth\":1}') }}", ""},
		{"with nonobject", "", "uses: actions/checkout@v6\n        with: ${{ 'wrong' }}", "with must be object"},
		{"with nonscalar property", "", "uses: actions/checkout@v6\n        with: ${{ fromJSON('{\"fetch-depth\":{}}') }}", "with.fetch-depth must be string"},
		{"services object", `services: ${{ fromJSON('{"db":{"image":"redis","command":"redis-server"}}') }}`, "run: echo test", ""},
		{"unknown expression object", "container: ${{ fromJSON(vars.CONTAINER) }}\n    strategy: ${{ fromJSON(vars.STRATEGY) }}", "run: echo ${{ matrix.os }}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    " + tc.job + "\n    steps:\n      - " + tc.step + "\n"
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleExpression(nil, nil)
			visitor := NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs = rule.Errs()
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatal(errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted one %q error, got %v", tc.want, errs)
			}
		})
	}
}

func TestSchemaAuditRunnerObjectExpressions(t *testing.T) {
	for _, tc := range []struct {
		value, want string
	}{
		{`fromJSON('{"group":"ubuntu-runners","labels":["linux"]}')`, ""},
		{`fromJSON('{"labels":"ubuntu-latest"}')`, ""},
		{`fromJSON('{"labels":{}}')`, "unsupported expression type"},
		{`fromJSON('{"unknown":"linux"}')`, "unknown property"},
		{`fromJSON(vars.RUNNERS)`, ""},
	} {
		t.Run(tc.value, func(t *testing.T) {
			workflow, errs := Parse([]byte("on: push\njobs:\n  test:\n    runs-on: ${{ " + tc.value + " }}\n    steps:\n      - run: echo test\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleExpression(nil, nil)
			visitor := NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs = rule.Errs()
			if tc.want == "" && len(errs) != 0 || tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want)) {
				t.Fatalf("wanted %q, got %v", tc.want, errs)
			}
		})
	}
}

func TestSchemaAuditExpressionDepth(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		valid            bool
	}{
		{"49 not operators", strings.Repeat("!", 49) + "false", true},
		{"50 not operators", strings.Repeat("!", 50) + "false", false},
		{"49 property accesses", "github" + strings.Repeat(".x", 49), true},
		{"50 property accesses", "github" + strings.Repeat(".x", 50), false},
		{"49 index accesses", "github" + strings.Repeat("[0]", 49), true},
		{"50 index accesses", "github" + strings.Repeat("[0]", 50), false},
		{"49 nested functions", strings.Repeat("format(", 49) + "'x'" + strings.Repeat(")", 49), true},
		{"50 nested functions", strings.Repeat("format(", 50) + "'x'" + strings.Repeat(")", 50), false},
		{"mixed functions and logical operators", strings.Repeat("format('x'||", 25) + "'x'" + strings.Repeat(")", 25), false},
		{"flattened or chain", strings.Repeat("true || ", 60) + "true", true},
		{"flattened and chain", strings.Repeat("true && ", 60) + "true", true},
		{"flattened parenthesized chain", strings.Repeat("true || (", 60) + "true" + strings.Repeat(")", 60), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExprParser().Parse(NewExprLexer(tc.expression + "}}"))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if err != nil && !strings.Contains(err.Message, "max expression depth 50") {
				t.Fatal(err)
			}
		})
	}
}
