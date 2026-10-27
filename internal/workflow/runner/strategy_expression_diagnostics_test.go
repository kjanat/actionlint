package runner_test

import (
	"fmt"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

func TestKnownStrategyRunnerLabels(t *testing.T) {
	for _, tc := range []struct {
		name, expression, runsOn string
		want                     []string
	}{
		{"axis typo", `fromJSON('{"matrix":{"os":["ubuntu-lates"]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"case insensitive", `fromJSON('{"Matrix":{"OS":["ubuntu-lates"]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"include typo", `fromJSON('{"matrix":{"include":[{"os":"ubuntu-lates"}]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"include case insensitive", `fromJSON('{"MATRIX":{"INCLUDE":[{"OS":"ubuntu-lates"}]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"axis plus include", `fromJSON('{"matrix":{"os":["ubuntu-lates"],"include":[{"os":"windows-lates"}]}}')`, "", []string{`label "ubuntu-lates" is unknown`, `label "windows-lates" is unknown`}},
		{"valid alternatives", `fromJSON('{"matrix":{"os":["ubuntu-latest","windows-latest"]}}')`, "", nil},
		{"valid include", `fromJSON('{"matrix":{"os":["ubuntu-latest"],"include":[{"os":"windows-latest"}]}}')`, "", nil},
		{"exclude adds no label", `fromJSON('{"matrix":{"os":["ubuntu-latest"],"exclude":[{"os":"ubuntu-lates"}]}}')`, "", nil},
		{"unknown strategy", `fromJSON(vars.STRATEGY)`, "", nil},
		{"invalid strategy shape", `fromJSON('[]')`, "", nil},
		{"missing matrix", `fromJSON('{}')`, "", nil},
		{"missing axis", `fromJSON('{"matrix":{"arch":["arm64"]}}')`, "", nil},
		{"invalid scalar axis", `fromJSON('{"matrix":{"os":"ubuntu-lates"}}')`, "", nil},
		{"include unrelated axis", `fromJSON('{"matrix":{"include":[{"arch":"arm64"}]}}')`, "", nil},
		{"numeric axis", `fromJSON('{"matrix":{"os":[1.5]}}')`, "", []string{`label "1.5" is unknown`}},
		{"boolean include", `fromJSON('{"matrix":{"include":[{"os":true}]}}')`, "", []string{`label "true" is unknown`}},
		{"null axis", `fromJSON('{"matrix":{"os":[null]}}')`, "", []string{`label "" is unknown`}},
		{"null include", `fromJSON('{"matrix":{"include":[{"os":null}]}}')`, "", []string{`label "" is unknown`}},
		{"empty axis", `fromJSON('{"matrix":{"os":[""]}}')`, "", []string{`label "" is unknown`}},
		{"empty include", `fromJSON('{"matrix":{"include":[{"os":""}]}}')`, "", []string{`label "" is unknown`}},
		{"array axis", `fromJSON('{"matrix":{"os":[["ubuntu-lates"]]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"array include", `fromJSON('{"matrix":{"include":[{"os":["ubuntu-lates"]}]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"runner object axis", `fromJSON('{"matrix":{"os":[{"labels":"ubuntu-lates"}]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"runner object include", `fromJSON('{"matrix":{"include":[{"os":{"labels":"ubuntu-lates"}}]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"runner object case insensitive", `fromJSON('{"matrix":{"os":[{"LABELS":["ubuntu-lates"]}]}}')`, "", []string{`label "ubuntu-lates" is unknown`}},
		{"group only runner", `fromJSON('{"matrix":{"os":[{"group":"custom-group"}]}}')`, "", nil},
		{"null in label array", `fromJSON('{"matrix":{"os":[[null]]}}')`, "", []string{`label "" is unknown`}},
		{"null runner labels", `fromJSON('{"matrix":{"os":[{"labels":null}]}}')`, "", []string{`label "" is unknown`}},
		{"conflicting array labels", `fromJSON('{"matrix":{"os":[["linux","windows"]]}}')`, "", []string{`label "windows" conflicts with label "linux"`}},
		{"conflicting object labels", `fromJSON('{"matrix":{"os":[{"labels":["linux","windows"]}]}}')`, "", []string{`label "windows" conflicts with label "linux"`}},
		{"independent array alternatives", `fromJSON('{"matrix":{"os":[["ubuntu-latest"],["windows-latest"]]}}')`, "", nil},
		{"independent include alternatives", `fromJSON('{"matrix":{"include":[{"os":["ubuntu-latest"]},{"os":{"labels":"windows-latest"}}]}}')`, "", nil},
		{"evaluated values remain data", `fromJSON('{"matrix":{"os":["${{ vars.RUNNER }}"]}}')`, "", []string{`label "${{ vars.RUNNER }}" is unknown`}},
		{"label interpolation", `fromJSON('{"matrix":{"os":["ubuntu-lates"]}}')`, `prefix-${{ matrix.os }}`, nil},
		{"unrelated context", `fromJSON('{"matrix":{"os":["ubuntu-lates"]}}')`, `${{ vars.RUNNER }}`, nil},
		{"conflict with fixed label", `fromJSON('{"matrix":{"os":["windows-latest"]}}')`, `[linux, "${{ matrix.os }}"]`, []string{`label "windows-latest" conflicts with label "linux"`}},
		{"self hosted alternatives", `fromJSON('{"matrix":{"os":["linux","windows"]}}')`, `[self-hosted, "${{ matrix.os }}"]`, nil},
		{"labels field", `fromJSON('{"matrix":{"os":["ubuntu-lates"]}}')`, `{group: example, labels: "${{ matrix.os }}"}`, []string{`label "ubuntu-lates" is unknown`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runsOn := tc.runsOn
			if runsOn == "" {
				runsOn = "${{ matrix.os }}"
			}
			source := fmt.Sprintf(`on: push
jobs:
  test:
    strategy: ${{ %s }}
    runs-on: %s
    steps:
      - run: echo ok
`, tc.expression, runsOn)
			_, errs := workflowlang.ParseDocument([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			errs = runnerlang.Check(runnerJobView(t, source), nil)
			if len(errs) != len(tc.want) {
				t.Fatalf("wanted %v, got %v", tc.want, errs)
			}
			for i, want := range tc.want {
				if got := errs[i]; got.Rule != "runner-label" || !strings.Contains(got.Message, want) || got.Start.Line != 4 || got.Start.Column != 15 {
					t.Fatalf("wanted %q at strategy expression 4:15, got %v", want, got)
				}
			}
		})
	}
}
