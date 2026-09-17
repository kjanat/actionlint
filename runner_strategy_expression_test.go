package actionlint

import (
	"fmt"
	"io"
	"strconv"
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

func TestRunnerMatrixCorrelatedLabels(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, runsOn string
		want                 []string
	}{
		{"include rows", `{"include":[{"os":"linux","image":"ubuntu-latest"},{"os":"windows","image":"windows-latest"}]}`, "", nil},
		{"case insensitive properties", `{"INCLUDE":[{"OS":"linux","IMAGE":"ubuntu-latest"},{"OS":"windows","IMAGE":"windows-latest"}]}`, "", nil},
		{"include row conflict", `{"include":[{"os":"linux","image":"windows-latest"},{"os":"windows","image":"windows-latest"}]}`, "", []string{`label "windows-latest" conflicts with label "linux"`}},
		{"independent axes", `{"os":["linux","windows"],"image":["ubuntu-latest","windows-latest"]}`, "", []string{`label "windows-latest" conflicts with label "linux"`, `label "ubuntu-latest" conflicts with label "windows"`}},
		{"exclude mismatches", `{"os":["linux","windows"],"image":["ubuntu-latest","windows-latest"],"exclude":[{"os":"linux","image":"windows-latest"},{"os":"windows","image":"ubuntu-latest"}]}`, "", nil},
		{"include extends matching originals", `{"os":["linux","windows"],"include":[{"os":"linux","image":"ubuntu-latest"},{"os":"windows","image":"windows-latest"}]}`, "", nil},
		{"case insensitive include filters", `{"os":["LINUX","WINDOWS"],"include":[{"os":"linux","image":"ubuntu-latest"},{"os":"windows","image":"windows-latest"}]}`, "", nil},
		{"case insensitive exclude filters", `{"os":["LINUX"],"image":["windows-latest"],"exclude":[{"os":"linux"}]}`, "", nil},
		{"partial object include preserves axis", `{"os":["windows"],"image":["ubuntu-latest"],"settings":[{"family":"linux","version":24}],"include":[{"settings":{"family":"linux"},"extra":"x64"}]}`, "", []string{`label "ubuntu-latest" conflicts with label "windows"`}},
		{"partial object include overrides extras", `{"os":["linux","windows"],"config":[{"name":"build","version":24}],"include":[{"image":"ubuntu-latest"},{"os":"windows","config":{"name":"build"},"image":"windows-latest"}]}`, "", nil},
		{"partial array include overrides extras", `{"os":["linux","windows"],"config":[["build",24]],"include":[{"image":"ubuntu-latest"},{"os":"windows","config":["build"],"image":"windows-latest"}]}`, "", nil},
		{"partial array excludes mismatch", `{"os":["linux","windows"],"image":["ubuntu-latest"],"config":[["build",24]],"exclude":[{"os":"windows","config":["build"]}]}`, "", nil},
		{"include overwrites added values", `{"os":["linux","windows"],"include":[{"image":"ubuntu-latest"},{"os":"windows","image":"windows-latest"}]}`, "", nil},
		{"include adds separate row", `{"os":["linux"],"image":["ubuntu-latest"],"include":[{"os":"windows","image":"windows-latest"}]}`, "", nil},
		{"appended rows remain separate", `{"os":["linux"],"image":["ubuntu-latest"],"include":[{"os":"windows","image":"windows-latest"},{"os":"windows","image":"ubuntu-latest"}]}`, "", []string{`label "ubuntu-latest" conflicts with label "windows"`}},
		{"include reintroduces excluded conflict", `{"os":["linux"],"image":["windows-latest"],"exclude":[{"os":"linux"}],"include":[{"os":"linux","image":"windows-latest"}]}`, "", []string{`label "windows-latest" conflicts with label "linux"`}},
		{"repeated property", `{"os":["linux","windows"]}`, `["${{ matrix.os }}", "${{ matrix.os }}"]`, nil},
		{"fixed label still conflicts", `{"include":[{"os":"windows","image":"x64"}]}`, `[linux, "${{ matrix.os }}", "${{ matrix.image }}"]`, []string{`label "windows" conflicts with label "linux"`}},
		{"unknown label remains checked", `{"include":[{"os":"linux","image":"ubuntu-lates"}]}`, "", []string{`label "ubuntu-lates" is unknown`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, expression := range []bool{false, true} {
				t.Run(fmt.Sprintf("expression=%v", expression), func(t *testing.T) {
					strategy := "strategy:\n      matrix: " + tc.matrix
					if expression {
						strategy = "strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}"
					}
					runsOn := tc.runsOn
					if runsOn == "" {
						runsOn = `["${{ matrix.os }}", "${{ matrix.image }}"]`
					}
					source := "on: push\njobs:\n  test:\n    " + strategy + "\n    runs-on: " + runsOn + "\n    steps:\n      - run: echo ok\n"
					linter, err := NewLinter(io.Discard, &LinterOptions{})
					if err != nil {
						t.Fatal(err)
					}
					errors, err := linter.Lint("test.yaml", []byte(source), nil)
					if err != nil {
						t.Fatal(err)
					}
					if len(errors) != len(tc.want) {
						t.Fatalf("wanted %v, got %v", tc.want, errors)
					}
					for _, want := range tc.want {
						found := false
						for _, diagnostic := range errors {
							if diagnostic.Kind == "runner-label" && strings.Contains(diagnostic.Message, want) {
								found = true
								if expression && (diagnostic.Line != 4 || diagnostic.Column != 15) {
									t.Fatalf("incorrect strategy diagnostic source: %v", diagnostic)
								}
							}
						}
						if !found {
							t.Fatalf("missing %q in %v", want, errors)
						}
					}
				})
			}
		})
	}
}

func TestRunnerMatrixFilterExpansion(t *testing.T) {
	// Matrix filters compare leaf values; include entries preserve original axes.
	// https://github.com/actions/runner/blob/759385a3510197a58b5c08dc1f373b74b9f4643b/src/Sdk/WorkflowParser/Conversion/MatrixBuilder.cs#L553-L620
	for _, tc := range []struct {
		name, matrix string
		want         []string
	}{
		{"object include", `{"config":[{"os":"linux","version":24}],"include":[{"config":{"os":"LINUX"},"label":"ubuntu-latest"}]}`, []string{`{"config": {"os": "linux", "version": 24}, "label": "ubuntu-latest"}`}},
		{"array include", `{"config":[["linux",24]],"include":[{"config":["LINUX"],"label":"ubuntu-latest"}]}`, []string{`{"config": ["linux", 24], "label": "ubuntu-latest"}`}},
		{"object exclude", `{"config":[{"os":"linux","version":24}],"exclude":[{"config":{"os":"LINUX"}}]}`, nil},
		{"array exclude", `{"config":[["linux",24]],"exclude":[{"config":["LINUX"]}]}`, nil},
		{"numeric string include", `{"version":[24],"include":[{"version":"24","label":"ubuntu-latest"}]}`, []string{`{"label": "ubuntu-latest", "version": 24}`}},
		{"numeric string exclude", `{"version":[24],"exclude":[{"version":"24"}]}`, nil},
		{"numeric bool exclude", `{"version":[1],"exclude":[{"version":true}]}`, nil},
		{"hex string exclude", `{"version":[24],"exclude":[{"version":"0x18"}]}`, nil},
		{"octal string exclude", `{"version":[24],"exclude":[{"version":"0o30"}]}`, nil},
		{"signed hex string exclude", `{"version":[-1],"exclude":[{"version":"0xffffffff"}]}`, nil},
		{"signed octal string exclude", `{"version":[-1],"exclude":[{"version":"0o37777777777"}]}`, nil},
		{"trimmed numeric string", `{"version":[24],"exclude":[{"version":" 24 "}]}`, nil},
		{"empty string excludes false", `{"version":[false],"exclude":[{"version":""}]}`, nil},
		{"null excludes zero", `{"version":[0],"exclude":[{"version":null}]}`, nil},
		{"different strings do not coerce", `{"version":["24"],"exclude":[{"version":"024"}]}`, []string{`{"version": "24"}`}},
		{"reject YAML null spelling", `{"version":[0],"exclude":[{"version":"null"}]}`, []string{`{"version": 0}`}},
		{"reject YAML bool spelling", `{"version":[1],"exclude":[{"version":"true"}]}`, []string{`{"version": 1}`}},
		{"reject underscores", `{"version":[24],"exclude":[{"version":"2_4"}]}`, []string{`{"version": 24}`}},
		{"reject uppercase radix prefix", `{"version":[24],"exclude":[{"version":"0X18"}]}`, []string{`{"version": 24}`}},
		{"reject hexadecimal floats", `{"version":[24],"exclude":[{"version":"0x1.8p4"}]}`, []string{`{"version": 24}`}},
		{"missing null leaf", `{"config":[{"os":"linux"}],"exclude":[{"config":{"version":null}}]}`, nil},
		{"array null past last index", `{"config":[["linux"]],"exclude":[{"config":["LINUX",null]}]}`, nil},
		{"empty nested filter", `{"config":[{"os":"linux"}],"exclude":[{"config":{}}]}`, nil},
		{"missing nonnull leaf", `{"config":[{"os":"linux"}],"exclude":[{"config":{"version":24}}]}`, []string{`{"config": {"os": "linux"}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, expression := range []bool{false, true} {
				strategy := "strategy:\n      matrix: " + tc.matrix
				if expression {
					strategy = "strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}"
				}
				source := "on: push\njobs:\n  test:\n    " + strategy + "\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				workflow, errs := Parse([]byte(source))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				combinations, known := knownRunnerMatrixCombinations(workflow.Jobs["test"].Strategy)
				if !known || len(combinations) != len(tc.want) {
					t.Fatalf("known=%v, wanted %v, got %v", known, tc.want, combinations)
				}
				for i, combination := range combinations {
					value := &RawYAMLObject{Props: combination}
					if got := value.String(); got != tc.want[i] {
						t.Fatalf("wanted %s, got %s", tc.want[i], got)
					}
				}
			}
		})
	}
}

func TestRunnerMatrixNumericFilters(t *testing.T) {
	for _, tc := range []struct {
		value, filter string
		matches       bool
	}{
		{".inf", "Infinity", true},
		{".inf", "infinity", true},
		{".inf", "+Infinity", true},
		{".inf", "1e999", true},
		{"-.inf", "-1e999", true},
		{".inf", ".inf", false},
		{".inf", "Inf", false},
		{".nan", "NaN", false},
		{"0", "1e-999", true},
	} {
		t.Run(tc.value+"_"+tc.filter, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    strategy:\n      matrix: {value: [" + tc.value + "], exclude: [{value: '" + tc.filter + "'}]}\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			combinations, known := knownRunnerMatrixCombinations(workflow.Jobs["test"].Strategy)
			if !known || (len(combinations) == 0) != tc.matches {
				t.Fatalf("known=%v, expected exclusion=%v, got %v", known, tc.matches, combinations)
			}
		})
	}
}

func TestRunnerMatrixCorrelationFallback(t *testing.T) {
	var extra []string
	for i := range 257 {
		extra = append(extra, strconv.Itoa(i))
	}
	for _, tc := range []struct {
		name, matrix, runsOn string
		want                 string
	}{
		{"unknown includes", `{os: [linux, windows], image: [ubuntu-latest, windows-latest], include: "${{ fromJSON(vars.INCLUDE) }}"}`, "", ""},
		{"unknown keeps fixed conflicts", `{os: [windows], image: [x64], include: "${{ fromJSON(vars.INCLUDE) }}"}`, `[linux, "${{ matrix.os }}", "${{ matrix.image }}"]`, `label "windows" conflicts with label "linux"`},
		{"unknown keeps label diagnostics", `{os: [linux], image: [ubuntu-lates], include: "${{ fromJSON(vars.INCLUDE) }}"}`, "", `label "ubuntu-lates" is unknown`},
		{"unknown matrix values", `{os: [linux, "${{ vars.OS }}"], image: [ubuntu-latest, windows-latest]}`, "", ""},
		{"bounded expansion keeps fixed conflicts", `{os: [windows], image: [x64], extra: [` + strings.Join(extra, ",") + `]}`, `[linux, "${{ matrix.os }}", "${{ matrix.image }}"]`, `label "windows" conflicts with label "linux"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runsOn := tc.runsOn
			if runsOn == "" {
				runsOn = `["${{ matrix.os }}", "${{ matrix.image }}"]`
			}
			source := "on: push\njobs:\n  test:\n    strategy:\n      matrix: " + tc.matrix + "\n    runs-on: " + runsOn + "\n    steps:\n      - run: echo ok\n"
			linter, err := NewLinter(io.Discard, &LinterOptions{})
			if err != nil {
				t.Fatal(err)
			}
			errors, err := linter.Lint("test.yaml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errors) != 0 {
					t.Fatal(errors)
				}
			} else if len(errors) != 1 || errors[0].Kind != "runner-label" || !strings.Contains(errors[0].Message, tc.want) {
				t.Fatalf("wanted one %q diagnostic, got %v", tc.want, errors)
			}
		})
	}
}

func TestKnownStrategyCorrelatedLabelsStayData(t *testing.T) {
	source := `on: push
jobs:
  test:
    strategy: ${{ fromJSON('{"matrix":{"include":[{"os":"${{ vars.RUNNER }}","image":"x64"}]}}') }}
    runs-on: ["${{ matrix.os }}", "${{ matrix.image }}"]
    steps:
      - run: echo ok
`
	linter, err := NewLinter(io.Discard, &LinterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	errors, err := linter.Lint("test.yaml", []byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || errors[0].Kind != "runner-label" || !strings.Contains(errors[0].Message, `label "${{ vars.RUNNER }}" is unknown`) {
		t.Fatalf("evaluated label data was reinterpreted: %v", errors)
	}
	if errors[0].Line != 4 || errors[0].Column != 15 {
		t.Fatalf("incorrect diagnostic source: %v", errors[0])
	}
}
