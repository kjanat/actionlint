package workflow_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

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
			request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
			var requestSources []workspace.Source
			var err error

			source := `on: push
jobs:
  test:
    strategy: ${{ fromJSON('{"matrix":` + tc.matrix + `}') }}
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo ok
`
			requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) != 1 || errs[0].Rule != "runner-label" || !strings.Contains(errs[0].Message, fmt.Sprintf("label %q is unknown", tc.label)) {
				t.Fatalf("wanted one runner-label diagnostic, got %v", errs)
			}
			if errs[0].Start.Line != 4 || errs[0].Start.Column != 15 || errs[0].Path != "test.yaml" {
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
			request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
			var requestSources []workspace.Source
			var err error

			source := `on: push
jobs:
  test:
    strategy: ${{ fromJSON('{"matrix":` + tc.matrix + `}') }}
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo ok
`
			requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.conflict {
				if len(errs) != 1 || errs[0].Rule != "runner-label" || !strings.Contains(errs[0].Message, `label "windows" conflicts with label "linux"`) || errs[0].Start.Line != 4 || errs[0].Start.Column != 15 {
					t.Fatalf("wanted one conflict at the strategy expression, got %v", errs)
				}
			} else if len(errs) != 0 {
				t.Fatalf("valid matrix runner alternatives rejected: %v", errs)
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
					source := `on: push
jobs:
  test:
    ` + strategy + "\n    runs-on: " + runsOn + `
    steps:
      - run: echo ok
`
					request := application.Options{}
					var requestSources []workspace.Source
					var err error
					requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
					errors, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
					if err != nil {
						t.Fatal(err)
					}
					if len(errors) != len(tc.want) {
						t.Fatalf("wanted %v, got %v", tc.want, errors)
					}
					for _, want := range tc.want {
						found := false
						for _, diagnostic := range errors {
							if diagnostic.Rule == "runner-label" && strings.Contains(diagnostic.Message, want) {
								found = true
								if expression && (diagnostic.Start.Line != 4 || diagnostic.Start.Column != 15) {
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
			source := `on: push
jobs:
  test:
    strategy:
      matrix: ` + tc.matrix + "\n    runs-on: " + runsOn + `
    steps:
      - run: echo ok
`
			request := application.Options{}
			var requestSources []workspace.Source
			var err error
			requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
			errors, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errors) != 0 {
					t.Fatal(errors)
				}
			} else if len(errors) != 1 || errors[0].Rule != "runner-label" || !strings.Contains(errors[0].Message, tc.want) {
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
	request := application.Options{}
	var requestSources []workspace.Source
	var err error
	requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
	errors, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
	if err != nil {
		t.Fatal(err)
	}
	if len(errors) != 1 || errors[0].Rule != "runner-label" || !strings.Contains(errors[0].Message, `label "${{ vars.RUNNER }}" is unknown`) {
		t.Fatalf("evaluated label data was reinterpreted: %v", errors)
	}
	if errors[0].Start.Line != 4 || errors[0].Start.Column != 15 {
		t.Fatalf("incorrect diagnostic source: %v", errors[0])
	}
}
