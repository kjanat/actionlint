package actionlint

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseActionOutlineDeclarations(t *testing.T) {
	const source = `name: Example
description: Test action
inputs:
  Zed:
    description: A required input with a default
    required: true
    default: ''
  Alpha:
    required: false
outputs:
  Value:
    description: Produced value
    value: ${{ steps.build.outputs.value }}
runs:
  using: composite
  steps:
    - id: build
      name: Build
      run: echo hidden-script-body
      shell: bash
    - uses: actions/checkout@v7
`
	outline, err := ParseActionOutline("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if outline.ParseStatus != "complete" || outline.Name != "Example" || outline.Description != "Test action" {
		t.Fatalf("wrong manifest: %+v", outline)
	}
	if len(outline.Inputs) != 2 || outline.Inputs[0].ID != "Zed" || outline.Inputs[1].ID != "Alpha" {
		t.Fatalf("input order/casing lost: %+v", outline.Inputs)
	}
	input := outline.Inputs[0]
	if input.Required == nil || !*input.Required || input.Default == nil || *input.Default != "" || input.Start == nil || *input.Start != (DiagnosticPosition{4, 3}) {
		t.Fatalf("declared input metadata lost: %+v", input)
	}
	if second := outline.Inputs[1]; second.Required == nil || *second.Required || second.Default != nil {
		t.Fatalf("omitted default/explicit false lost: %+v", second)
	}
	if len(outline.Outputs) != 1 || outline.Outputs[0].ID != "Value" || outline.Outputs[0].Value == nil || *outline.Outputs[0].Value != "${{ steps.build.outputs.value }}" {
		t.Fatalf("output declaration lost: %+v", outline.Outputs)
	}
	runs, ok := outline.Runs.(CompositeRuns)
	if !ok || len(runs.Steps) != 2 || runs.Steps[0].Kind != "run" || runs.Steps[0].ID != "build" || runs.Steps[1].Reference == nil {
		t.Fatalf("composite declarations lost: %+v", outline.Runs)
	}
	encoded, err := json.Marshal(outline)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "hidden-script-body") {
		t.Fatal("outline leaked script body")
	}
	var decoded ActionOutline
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, outline) {
		t.Fatalf("action roundtrip changed data: got %+v, want %+v", decoded, outline)
	}
}

func TestParseActionOutlineRuntimes(t *testing.T) {
	for _, tc := range []struct {
		name, runs string
		want       ActionRuns
	}{
		{"javascript", "using: node24\n  main: main.mjs\n  pre: pre.mjs\n  pre-if: always()\n  post: post.mjs\n  post-if: success()", JavaScriptRuns{Using: "node24", Main: "main.mjs", Pre: "pre.mjs", PreIf: "always()", Post: "post.mjs", PostIf: "success()"}},
		{"docker", "using: docker\n  image: docker://alpine:3\n  entrypoint: /entry.sh\n  pre-entrypoint: /pre.sh\n  post-entrypoint: /post.sh\n  args: [test, 42]", DockerRuns{Image: "docker://alpine:3", Entrypoint: "/entry.sh", PreEntrypoint: "/pre.sh", PostEntrypoint: "/post.sh", Args: []string{"test", "42"}}},
		{"plugin", "plugin: GitHub.Runner.Plugins.Repository.CheckoutAction", PluginRuns{Plugin: "GitHub.Runner.Plugins.Repository.CheckoutAction"}},
		{"empty composite", "using: composite\n  steps: []", CompositeRuns{Steps: []StepOutline{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outline, err := ParseActionOutline("action.yaml", []byte("name: Test\ndescription: Test action\nruns:\n  "+tc.runs+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(outline.Runs, tc.want) {
				t.Fatalf("runtime: got %+v, want %+v", outline.Runs, tc.want)
			}
			encoded, err := json.Marshal(outline)
			if err != nil {
				t.Fatal(err)
			}
			var decoded ActionOutline
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, outline) {
				t.Fatalf("runtime roundtrip changed data: %s", encoded)
			}
		})
	}
}

func TestParseActionOutlinePartial(t *testing.T) {
	for _, tc := range []struct {
		name, source, status string
	}{
		{"invalid YAML", "runs: [", "failed"},
		{"empty", "", "failed"},
		{"invalid inputs", "name: Keep\ninputs: [wrong]\nruns: {using: node24, main: main.mjs}\n", "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outline, err := ParseActionOutline("action.yml", []byte(tc.source))
			if err == nil || outline.ParseStatus != tc.status {
				t.Fatalf("status=%q error=%v; wanted %q with error", outline.ParseStatus, err, tc.status)
			}
			if tc.status == "partial" && outline.Name != "Keep" {
				t.Fatalf("partial name lost: %+v", outline)
			}
			if tc.name == "invalid inputs" {
				if runs, ok := outline.Runs.(JavaScriptRuns); !ok || runs.Main != "main.mjs" {
					t.Fatalf("early decode error lost later runs declaration: %+v", outline.Runs)
				}
			}
			if outline.Inputs == nil || outline.Outputs == nil || outline.Runs == nil {
				t.Fatalf("incomplete action has nil collections/runtime: %+v", outline)
			}
		})
	}
}

func TestActionOutlineParseStatusDoesNotValidate(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"missing identity", "runs: {using: node24, main: main.mjs}"},
		{"unexpected runs key", "runs: {using: node24, main: main.mjs, unexpected: value}"},
		{"missing runtime", "runs: {main: main.mjs}"},
		{"unknown runtime", "runs: {using: future-runtime}"},
		{"missing JavaScript entrypoint", "runs: {using: node24}"},
		{"empty JavaScript entrypoint", "runs: {using: node24, main: ''}"},
		{"missing Docker image", "runs: {using: docker}"},
		{"empty Docker image", "runs: {using: docker, image: ''}"},
		{"missing composite steps", "runs: {using: composite}"},
		{"null composite steps", "runs: {using: composite, steps: null}"},
		{"conflicting runtime", "runs: {using: node24, main: main.mjs, image: docker://alpine:3}"},
		{"conflicting step", "runs: {using: composite, steps: [{run: echo hi, uses: actions/checkout@v7, shell: bash}]}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outline, err := ParseActionOutline("action.yml", []byte(tc.source))
			if err != nil || outline.ParseStatus != "complete" {
				t.Fatalf("decoded declarations treated as validation failure: status=%q error=%v", outline.ParseStatus, err)
			}
			if tc.name == "unknown runtime" {
				if runtime, ok := outline.Runs.(UnknownRuns); !ok || runtime.Using != "future-runtime" {
					t.Fatalf("unknown classification lost declaration: %+v", outline.Runs)
				}
			}
			if tc.name == "conflicting step" {
				runtime, ok := outline.Runs.(CompositeRuns)
				if !ok || len(runtime.Steps) != 1 || runtime.Steps[0].Kind != "unknown" || runtime.Steps[0].Uses != "actions/checkout@v7" || runtime.Steps[0].Reference == nil {
					t.Fatalf("conflicting step lost declarations or invented execution: %+v", outline.Runs)
				}
			}
		})
	}
}

func TestParseActionOutlineRequiredBooleans(t *testing.T) {
	for _, tc := range []struct {
		literal string
		want    bool
	}{
		{"true", true}, {"false", false}, {"yes", true}, {"No", false}, {"on", true}, {"OFF", false},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			source := "name: Test\ndescription: Test action\ninputs:\n  First:\n    required: &required " + tc.literal + "\n    default: ''\n  Second:\n    required: *required\nruns: {using: node24, main: main.mjs}\n"
			outline, err := ParseActionOutline("action.yml", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if len(outline.Inputs) != 2 {
				t.Fatalf("inputs lost: %+v", outline.Inputs)
			}
			for _, input := range outline.Inputs {
				if input.Required == nil || *input.Required != tc.want {
					t.Fatalf("required %s: got %+v, want %t", tc.literal, input, tc.want)
				}
			}
		})
	}
}
