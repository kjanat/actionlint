package runner_test

import (
	"strings"
	"testing"

	runnerlang "actionlint.kjanat.dev/internal/workflow/runner"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestKnownExpressionRunnerLabels(t *testing.T) {
	for _, tc := range []struct {
		name, runsOn, want string
	}{
		{"object typo", `${{ fromJSON('{"labels":["ubuntu-lates"]}') }}`, `label "ubuntu-lates" is unknown`},
		{"array typo", `${{ fromJSON('["ubuntu-lates"]') }}`, `label "ubuntu-lates" is unknown`},
		{"string typo", `${{ 'ubuntu-lates' }}`, `label "ubuntu-lates" is unknown`},
		{"valid object", `${{ fromJSON('{"labels":["ubuntu-latest"]}') }}`, ""},
		{"casing", `${{ fromJSON('{"LABELS":["UBUNTU-LATEST"]}') }}`, ""},
		{"group is not label", `${{ fromJSON('{"group":"custom-group"}') }}`, ""},
		{"group and labels", `${{ fromJSON('{"group":"custom-group","labels":"linux"}') }}`, ""},
		{"group does not hide typo", `${{ fromJSON('{"group":"custom-group","labels":"ubuntu-lates"}') }}`, `label "ubuntu-lates" is unknown`},
		{"conflicting array", `${{ fromJSON('["ubuntu-latest","windows-latest"]') }}`, `label "windows-latest" conflicts`},
		{"dynamic", `${{ fromJSON(vars.RUNNERS) }}`, ""},
		{"interpolation", `prefix-${{ 'ubuntu-lates' }}`, ""},
		{"invalid object label handled by expression rule", `${{ fromJSON('{"labels":[{}]}') }}`, ""},
		{"null handled by expression rule", `${{ fromJSON('{"labels":[null]}') }}`, ""},
		{"number coercion", `${{ fromJSON('{"labels":[42]}') }}`, `label "42" is unknown`},
		{"boolean coercion", `${{ fromJSON('{"labels":[false]}') }}`, `label "false" is unknown`},
		{"labels expression", `{labels: "${{ fromJSON('[\"ubuntu-lates\"]') }}"}`, `label "ubuntu-lates" is unknown`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, parseErrors := workflowlang.Parse([]byte(`on: push
jobs:
  test:
    runs-on: ` + tc.runsOn + `
    steps:
      - run: echo ok
`))
			if len(parseErrors) != 0 {
				t.Fatal(parseErrors)
			}
			errs := runnerlang.Check(workflow.Jobs["test"], nil)
			if tc.want == "" && len(errs) != 0 || tc.want != "" && (len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want)) {
				t.Fatalf("wanted %q, got %v", tc.want, errs)
			}
		})
	}
}
