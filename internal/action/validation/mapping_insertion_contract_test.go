package validation_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestInsertedActionInputMetadata(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"supplies required", "{token: value}", ""},
		{"constant input name", `{ "${{ 'token' }}": value }`, ""},
		{"case-insensitive input name", `{ "${{ 'TOKEN' }}": value }`, ""},
		{"nested constant input name", `{'${{ insert }}': {"${{ 'token' }}": value}}`, ""},
		{"unresolved input name", `{'${{ vars.NAME }}': value}`, ""},
		{"unresolved name retains known checks", `{'${{ vars.NAME }}': value, typo: value}`, `input "typo" is not defined`},
		{"constant unknown input name", `{token: value, "${{ 'typo' }}": value}`, `input "typo" is not defined`},
		{"known object supplies required", `${{ fromJSON('{"token":"value"}') }}`, ""},
		{"unknown can supply required", "${{ fromJSON(vars.EXTRA) }}", ""},
		{"known empty misses required", "{}", `missing input "token"`},
		{"unknown name", "{token: value, typo: value}", `input "typo" is not defined`},
		{"known expression name", `${{ fromJSON('{"token":"value","typo":"value"}') }}`, `input "typo" is not defined`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := workflowlang.ParseDocument([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n        with:\n          ${{ insert }}: " + tc.value + "\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			exec := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecAction)
			got := actioncheck.Inputs(&actionmeta.Metadata{Inputs: map[string]*actionmeta.Input{"token": {Name: "token", Required: true}}}, exec, func(*actionmeta.Metadata) string { return "example/action@v1" })
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatal(got)
				}
			} else if len(got) != 1 || !strings.Contains(got[0].Message, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, got)
			}
		})
	}
}
