package actionlint

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
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
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: ".github/workflows/consumer.yml", Content: []byte(workflow), Project: &Project{root: root}}}})
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
		cfg, err := ParseConfig([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		source := `on: {workflow_run: {workflows: ['?Build'], types: [completed]}}
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`
		result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: filepath.Join("outside", "workflow.yml"), Content: []byte(source), Config: cfg}}})
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

func TestWorkflowRunGlobUnknownExpression(t *testing.T) {
	rule := NewRuleGlob()
	workflow := &Workflow{On: []Event{&WebhookEvent{Workflows: []*String{{Value: "${{ inputs.pattern }}", Pos: &Pos{Line: 1, Col: 1}}}}}}
	if err := rule.VisitWorkflowPre(workflow); err != nil || len(rule.Errs()) != 0 {
		t.Fatalf("unknown expression treated as a literal glob: %v, %v", rule.Errs(), err)
	}
}

func TestWorkflowRunGlobRequiresPositivePattern(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		invalid  bool
	}{
		{[]string{"!Build"}, true},
		{[]string{"!Build", "!Test*"}, true},
		{[]string{"${{ '!Build' }}"}, true},
		{[]string{"Build", "!Test*"}, false},
		{[]string{"Build*", "!Build docs"}, false},
		{[]string{`\!Build`}, false},
		{[]string{"!Build", "${{ inputs.workflow }}"}, false},
	} {
		t.Run(strings.Join(tc.patterns, ","), func(t *testing.T) {
			var patterns []string
			for _, pattern := range tc.patterns {
				patterns = append(patterns, strconv.Quote(pattern))
			}
			source := `on:
  workflow_run:
    workflows: [` + strings.Join(patterns, ", ") + `]
    types: [completed]
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleGlob()
			if err := rule.VisitWorkflowPre(workflow); err != nil {
				t.Fatal(err)
			}
			if got := rule.Errs(); (len(got) != 0) != tc.invalid || len(got) > 1 || len(got) == 1 && (got[0].Line != 3 || !strings.Contains(got[0].Message, "positive")) {
				t.Fatalf("positive-pattern validation: %+v", got)
			}
		})
	}
}
