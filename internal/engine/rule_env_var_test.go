package engine

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func testValidateEnvVarName(t *testing.T, name string) []*Error {
	t.Helper()
	n := &workflowlang.String{Value: name, Pos: &workflowlang.Pos{}}
	e := &workflowlang.Env{
		Vars: map[string]*workflowlang.EnvVar{
			name: {Name: n, Value: &workflowlang.String{Value: "", Pos: &workflowlang.Pos{}}},
		},
	}
	w := &workflowlang.Workflow{Env: e}
	r := NewRuleEnvVar()
	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	return r.Errs()
}

func TestRuleEnvVarCheckValidName(t *testing.T) {
	valids := []string{
		"foo_bar",
		"FOO_BAR",
		"foo-bar",
		"_",
		"-",
	}

	for _, n := range valids {
		t.Run(n, func(t *testing.T) {
			if errs := testValidateEnvVarName(t, n); len(errs) > 0 {
				t.Fatalf("env var %q should be valid but got errors: %v", n, errs)
			}
		})
	}
}

func TestRuleEnvVarCheckInvalidName(t *testing.T) {
	invalids := []string{
		"a b",
		"a=b",
		"a=b",
	}

	for _, n := range invalids {
		t.Run(n, func(t *testing.T) {
			if errs := testValidateEnvVarName(t, n); len(errs) == 0 {
				t.Fatalf("env var %q should be invalid but got no error", n)
			}
		})
	}
}

func TestRuleEnvVarSkipEdgeCaseEnv(t *testing.T) {
	w := &workflowlang.Workflow{Env: nil}
	r := NewRuleEnvVar()
	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := r.Errs(); len(errs) > 0 {
		t.Fatal("no error should be detected when `Env` is nil but got", errs)
	}

	w = &workflowlang.Workflow{
		Env: &workflowlang.Env{
			Expression: &workflowlang.String{Value: ""},
		},
	}
	r = NewRuleEnvVar()
	if err := r.VisitWorkflowPre(w); err != nil {
		t.Fatal(err)
	}
	if errs := r.Errs(); len(errs) > 0 {
		t.Fatal("no error should be detected when `Env` is constructed by expression", errs)
	}
}
