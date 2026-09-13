package actionlint

import (
	"fmt"
	"io"
	"strings"
	"testing"
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
			source := fmt.Sprintf("on: push\njobs:\n  test:\n    strategy: ${{ %s }}\n    runs-on: %s\n    steps:\n      - run: echo ok\n", tc.expression, runsOn)
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleRunnerLabel()
			visitor := NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs = rule.Errs()
			if len(errs) != len(tc.want) {
				t.Fatalf("wanted %v, got %v", tc.want, errs)
			}
			for i, want := range tc.want {
				if got := errs[i]; got.Kind != "runner-label" || !strings.Contains(got.Message, want) || got.Line != 4 || got.Column != 15 {
					t.Fatalf("wanted %q at strategy expression 4:15, got %v", want, got)
				}
			}
		})
	}
}

func TestKnownStrategyRunnerLabelsLint(t *testing.T) {
	for _, tc := range []struct{ matrix, label string }{
		{`{"os":["ubuntu-lates"]}`, "ubuntu-lates"},
		{`{"include":[{"os":"ubuntu-lates"}]}`, "ubuntu-lates"},
		{`{"os":[null]}`, ""},
		{`{"include":[{"os":null}]}`, ""},
		{`{"os":[""]}`, ""},
		{`{"include":[{"os":""}]}`, ""},
		{`{"os":[["ubuntu-lates"]]}`, "ubuntu-lates"},
		{`{"include":[{"os":["ubuntu-lates"]}]}`, "ubuntu-lates"},
		{`{"os":[{"labels":"ubuntu-lates"}]}`, "ubuntu-lates"},
		{`{"include":[{"os":{"labels":"ubuntu-lates"}}]}`, "ubuntu-lates"},
		{`{"os":[[null]]}`, ""},
		{`{"include":[{"os":{"labels":null}}]}`, ""},
	} {
		t.Run(tc.matrix, func(t *testing.T) {
			lint, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			source := "on: push\njobs:\n  test:\n    strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}\n    runs-on: ${{ matrix.os }}\n    steps:\n      - run: echo ok\n"
			errs, err := lint.Lint("test.yaml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) != 1 || errs[0].Kind != "runner-label" || !strings.Contains(errs[0].Message, fmt.Sprintf("label %q is unknown", tc.label)) {
				t.Fatalf("wanted one runner-label diagnostic, got %v", errs)
			}
			if errs[0].Line != 4 || errs[0].Column != 15 || errs[0].Filepath != "test.yaml" {
				t.Fatalf("incorrect diagnostic source: %v", errs[0])
			}
		})
	}
}

func TestKnownStrategyRunnerGroupsLint(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		conflict     bool
	}{
		{"array conflict", `{"os":[["linux","windows"]]}`, true},
		{"object conflict", `{"os":[{"labels":["linux","windows"]}]}`, true},
		{"include conflict", `{"include":[{"os":["linux","windows"]}]}`, true},
		{"array alternatives", `{"os":[["ubuntu-latest"],["windows-latest"]]}`, false},
		{"include alternatives", `{"include":[{"os":["ubuntu-latest"]},{"os":{"labels":"windows-latest"}}]}`, false},
		{"group only", `{"os":[{"group":"custom-group"}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lint, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
			if err != nil {
				t.Fatal(err)
			}
			source := "on: push\njobs:\n  test:\n    strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}\n    runs-on: ${{ matrix.os }}\n    steps:\n      - run: echo ok\n"
			errs, err := lint.Lint("test.yaml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.conflict {
				if len(errs) != 1 || errs[0].Kind != "runner-label" || !strings.Contains(errs[0].Message, `label "windows" conflicts with label "linux"`) || errs[0].Line != 4 || errs[0].Column != 15 {
					t.Fatalf("wanted one conflict at the strategy expression, got %v", errs)
				}
			} else if len(errs) != 0 {
				t.Fatalf("valid matrix runner alternatives rejected: %v", errs)
			}
		})
	}
}

func TestRunnerMatrixIncludeExcludeParity(t *testing.T) {
	for _, tc := range []struct {
		name, matrix string
		want         int
	}{
		{"include only", `{"include":[{"os":"ubuntu-lates"}]}`, 1},
		{"axis and include", `{"os":["ubuntu-lates"],"include":[{"os":"windows-lates"}]}`, 2},
		{"exclude introduces no labels", `{"os":["ubuntu-latest"],"exclude":[{"os":"windows-lates"}]}`, 0},
		{"declarations checked before exclusions", `{"os":["ubuntu-lates"],"exclude":[{"os":"ubuntu-lates"}]}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []string
			for _, strategy := range []string{
				"strategy:\n      matrix: " + tc.matrix,
				"strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}",
			} {
				source := "on: push\njobs:\n  test:\n    " + strategy + "\n    runs-on: ${{ matrix.os }}\n    steps:\n      - run: echo ok\n"
				workflow, errs := Parse([]byte(source))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				rule := NewRuleRunnerLabel()
				if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
					t.Fatal(err)
				}
				errs = rule.Errs()
				if len(errs) != tc.want {
					t.Fatalf("wanted %d runner-label diagnostics, got %v", tc.want, errs)
				}
				if messages == nil {
					messages = make([]string, len(errs))
					for i, err := range errs {
						messages[i] = err.Message
					}
				} else {
					for i, err := range errs {
						if err.Message != messages[i] {
							t.Fatalf("literal/expression mismatch: %q != %q", err.Message, messages[i])
						}
					}
				}
			}
		})
	}
}
