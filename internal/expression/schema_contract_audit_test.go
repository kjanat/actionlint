package expression_test

import (
	"slices"
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/internal/expression"
	contextspec "actionlint.kjanat.dev/internal/expression/context"
)

// The context profiles follow runner 759385a3510197a58b5c08dc1f373b74b9f4643b
// and languageservices 4043eda158e16579cc5fb1b0b07a4bce2a76f0b5.
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
				contexts, functions := contextspec.WorkflowKeyAvailability(key)
				if !slices.Equal(contexts, strings.Fields(profile.contexts)) || !slices.Equal(functions, strings.Fields(profile.funcs)) {
					t.Fatalf("contexts %v; functions %v", contexts, functions)
				}
				if !slices.Contains(contextspec.AllWorkflowKeys, key) {
					t.Fatal("missing from generated workflow key inventory")
				}
				for context := range exprlang.BuiltinGlobalVariableTypes {
					checker := exprlang.NewChecker(false, nil)
					checker.SetWorkflowKeyAvailability(key)
					expr, err := exprlang.NewParser().Parse(exprlang.NewLexer("toJSON(" + context + ")}}"))
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
	for _, key := range contextspec.AllWorkflowKeys {
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
			expr, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := exprlang.NewChecker(false, nil)
			checker.SetWorkflowKeyAvailability(tc.key)
			_, errs := checker.Check(expr)
			if (len(errs) == 0) != tc.valid {
				t.Fatalf("valid=%v, diagnostics=%v", tc.valid, errs)
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
			_, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.expression + "}}"))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if err != nil && !strings.Contains(err.Message, "max expression depth 50") {
				t.Fatal(err)
			}
		})
	}
}
