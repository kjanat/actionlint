package workflow_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestRunnerExpressionNonemptyLabels(t *testing.T) {
	for _, tc := range []struct {
		name, value, kind string
	}{
		{"top-level empty", "${{ fromJSON('[]') }}", "expression"},
		{"empty labels", `${{ fromJSON('{"labels":[]}') }}`, "expression"},
		{"empty labels with group", `${{ fromJSON('{"group":"pool","labels":[]}') }}`, "expression"},
		{"mixed case empty labels", `${{ fromJSON('{"Labels":[]}') }}`, "expression"},
		{"labels field", `{labels: "${{ fromJSON('[]') }}"}`, "expression"},
		{"empty scalar retains its diagnostic", `{labels: "${{ '' }}"}`, "runner-label"},
		{"nonempty", `${{ fromJSON('["ubuntu-latest"]') }}`, ""},
		{"nonempty object", `${{ fromJSON('{"labels":["ubuntu-latest"]}') }}`, ""},
		{"group without labels", `${{ fromJSON('{"group":"pool"}') }}`, ""},
		{"unknown runner", "${{ fromJSON(vars.RUNNER) }}", ""},
		{"unknown labels", `{labels: "${{ fromJSON(vars.LABELS) }}"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := application.Options{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
			var requestSources []workspace.Source
			var err error

			source := `on: push
jobs:
  build:
    runs-on: ` + tc.value + `
    steps:
      - run: echo ok
`
			requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind != "" {
				if len(errs) != 1 || errs[0].Rule != tc.kind {
					t.Fatalf("want one nonempty-labels diagnostic, got %v", errs)
				}
				if tc.kind == "expression" && !strings.Contains(errs[0].Message, "must contain at least one") {
					t.Fatalf("unexpected diagnostic: %v", errs[0])
				}
			} else if len(errs) != 0 {
				t.Fatalf("valid or unknown labels rejected: %v", errs)
			}
		})
	}
}
