package rules

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/input/source"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/results/finding"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func testValidateEnvVarName(t *testing.T, name string) []*finding.Finding {
	t.Helper()
	n := &definition.String{Value: name, Pos: &source.Position{}}
	e := &definition.Env{
		Vars: map[string]*definition.EnvVar{
			name: {Name: n, Value: &definition.String{Value: "", Pos: &source.Position{}}},
		},
	}
	w := &definition.Workflow{Env: e}
	r := correctness.EnvVarRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
	if err := r.VisitWorkflowPre(declarations.Workflow(w)); err != nil {
		t.Fatal(err)
	}
	return r.Errs()
}

func TestRuleEnvVarCheckValidName(t *testing.T) {
	t.Parallel()
	valids := []string{
		"foo_bar",
		"FOO_BAR",
		"foo-bar",
		"_",
		"-",
	}

	for _, n := range valids {
		t.Run(n, func(t *testing.T) {
			t.Parallel()
			if errs := testValidateEnvVarName(t, n); len(errs) > 0 {
				t.Fatalf("env var %q should be valid but got errors: %v", n, errs)
			}
		})
	}
}

func TestRuleEnvVarCheckInvalidName(t *testing.T) {
	t.Parallel()
	invalids := []string{
		"a b",
		"a=b",
		"a=b",
	}

	for _, n := range invalids {
		t.Run(n, func(t *testing.T) {
			t.Parallel()
			if errs := testValidateEnvVarName(t, n); len(errs) == 0 {
				t.Fatalf("env var %q should be invalid but got no error", n)
			}
		})
	}
}

func TestRuleEnvVarSkipEdgeCaseEnv(t *testing.T) {
	t.Parallel()
	w := &definition.Workflow{Env: nil}
	r := correctness.EnvVarRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
	if err := r.VisitWorkflowPre(declarations.Workflow(w)); err != nil {
		t.Fatal(err)
	}
	if errs := r.Errs(); len(errs) > 0 {
		t.Fatal("no error should be detected when `Env` is nil but got", errs)
	}

	w = &definition.Workflow{
		Env: &definition.Env{
			Expression: &definition.String{Value: ""},
		},
	}
	r = correctness.EnvVarRule.NewVisitor(define.RuleInputs{Rules: checking.ConfigurationRules()})
	if err := r.VisitWorkflowPre(declarations.Workflow(w)); err != nil {
		t.Fatal(err)
	}
	if errs := r.Errs(); len(errs) > 0 {
		t.Fatal("no error should be detected when `Env` is constructed by expression", errs)
	}
}
