package engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestWorkflowRunGlobDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		pattern, producer, rule string
	}{
		{"Build [", "Build", "glob"},
		{"?Build", "Build", "glob"},
		{"Build [z-a]", "Build", "glob"},
		{"Build [A-z]", "Build", "glob"},
		{"Build [ab_]", "Build", "glob"},
		{"!Build [", "Build", "glob"},
		{"Build*", "Build CI", ""},
		{"Missing*", "Build CI", "workflow-run-names"},
		{"Build [a]", "Build a", ""},
		{"Build: CI", "Build: CI", ""},
		{" Build ", " Build ", ""},
		{"./Build", "./Build", ""},
		{`\[Build\]`, "[Build]", ""},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			root := t.TempDir()
			producer := writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: '"+tc.producer+"'\n"+commandGoodWorkflow)
			workflow := `on:
  workflow_run:
    workflows: ['` + tc.pattern + `']
    types: [completed]
jobs:
  test:
    runs-on: ubuntu-latest
    steps: [{run: echo ok}]
`
			result, err := runTestAnalysis(t.Context(), Request{WorkingDir: root, Sources: []SourceUnit{{Path: ".github/workflows/consumer.yml", Content: []byte(workflow), Project: workspace.Identity(root)}}})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.rule != "" {
				want = 1
			}
			if len(result.Diagnostics) != want || (want == 1 && (result.Diagnostics[0].Rule != tc.rule || result.Diagnostics[0].Start.Line != 3)) {
				t.Fatalf("want %d %s findings, got %+v", want, tc.rule, result.Diagnostics)
			}
			if !slices.Contains(result.Inputs, producer) {
				t.Fatalf("repository producer lost from inputs: %v", result.Inputs)
			}
		})
	}
}

func TestWorkflowRunGlobWithoutProject(t *testing.T) {
	for _, config := range []string{"", "lint: {rules: {correctness: {glob: off}}}"} {
		cfg, err := repoconfig.Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		source := `on: {workflow_run: {workflows: ['?Build'], types: [completed]}}
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`
		result, err := runTestAnalysis(t.Context(), Request{Sources: []SourceUnit{{Path: filepath.Join("outside", "workflow.yml"), Content: []byte(source), Config: cfg}}})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if strings.Contains(config, "off") {
			want = 0
		}
		if len(result.Diagnostics) != want || (want == 1 && result.Diagnostics[0].Rule != "glob") {
			t.Fatalf("project-free glob validation: %+v", result.Diagnostics)
		}
	}
}
