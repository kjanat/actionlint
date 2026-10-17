package validation_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	actioncheck "actionlint.kjanat.dev/internal/action/validation"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestKnownExpressionActionInputs(t *testing.T) {
	meta := &actionmeta.Metadata{Inputs: actionmeta.Inputs{
		"token": {Name: "token", Required: true},
		"old":   {Name: "old", Deprecated: true},
	}}
	for _, tc := range []struct {
		name, expression, want string
	}{
		{"missing", `fromJSON('{}')`, `missing input "token"`},
		{"provided", `fromJSON('{"token":"value"}')`, ""},
		{"case insensitive", `fromJSON('{"TOKEN":"value"}')`, ""},
		{"scalar coercion", `fromJSON('{"token":false}')`, ""},
		{"unknown input", `fromJSON('{"token":"value","typo":"x"}')`, `input "typo" is not defined`},
		{"deprecated input", `fromJSON('{"token":"value","old":"x"}')`, `avoid using deprecated input "old"`},
		{"dynamic", `fromJSON(vars.INPUTS)`, ""},
		{"invalid shape handled by expression rule", `fromJSON('[]')`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, parseErrors := workflowlang.Parse([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: example/action@v1
        with: ${{ ` + tc.expression + " }}\n"))
			if len(parseErrors) != 0 {
				t.Fatal(parseErrors)
			}
			exec := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecAction)
			errs := actioncheck.Inputs(meta, exec, func(*actionmeta.Metadata) string { return "example/action@v1" })
			if tc.want == "" && len(errs) != 0 || tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want)) {
				t.Fatalf("wanted %q, got %v", tc.want, errs)
			}
		})
	}
}
