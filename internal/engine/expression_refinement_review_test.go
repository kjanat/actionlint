package engine

import (
	"fmt"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/internal/action"
	"actionlint.kjanat.dev/internal/tools/adapters"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

func TestExpressionRefinementReview(t *testing.T) {
	for _, tc := range []struct{ name, field, want string }{
		{"queue integer", "concurrency: {group: build, queue: '${{ 1 }}'}", "must be single or max"},
		{"queue float", "concurrency: {group: build, queue: '${{ 1.5 }}'}", "must be single or max"},
		{"queue boolean", "concurrency: {group: build, queue: '${{ true }}'}", "must be single or max"},
		{"queue null", "concurrency: {group: build, queue: '${{ null }}'}", "must be single or max"},
		{"environment prefix", "environment: pre-${{ '' }}", ""},
		{"environment suffix", "environment: ${{ '' }}-post", ""},
		{"environment multiple", "environment: ${{ '' }}-${{ '' }}", ""},
		{"environment null", "environment: ${{ null }}", "must be a non-empty string"},
		{"environment unknown", "environment: ${{ vars.ENVIRONMENT }}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := Request{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
			var err error

			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    ` + tc.field + `
    steps:
      - run: echo ok
`
			request.Sources = []SourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
			errs, err := testFindings(runTestAnalysis(t.Context(), request))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("want one %q diagnostic, got %v", tc.want, errs)
			}
		})
	}
}

func TestConcurrencyExpressionConflictReview(t *testing.T) {
	for _, scope := range []string{"workflow", "job"} {
		for _, tc := range []struct {
			queue, cancel string
			conflict      bool
		}{
			{"max", "true", true},
			{"${{ 'max' }}", "true", true},
			{"max", "${{ true }}", true},
			{"${{ fromJSON('\"max\"') }}", "${{ fromJSON('true') }}", true},
			{"${{ 'max' }}", "false", false},
			{"${{ 'single' }}", "true", false},
			{"${{ vars.QUEUE }}", "true", false},
			{"max", "${{ github.ref == 'refs/heads/main' }}", false},
		} {
			t.Run(scope+"/"+tc.queue+"/"+tc.cancel, func(t *testing.T) {
				field := fmt.Sprintf(`concurrency:
  group: build
  queue: %s
  cancel-in-progress: %s
`, tc.queue, tc.cancel)
				source := "on: push\n"
				if scope == "workflow" {
					source += field
				}
				source += `jobs:
  test:
    runs-on: ubuntu-latest
`
				if scope == "job" {
					source += "    " + strings.ReplaceAll(strings.TrimSuffix(field, "\n"), "\n", "\n    ") + "\n"
				}
				source += `    steps:
      - run: echo ok
`
				request := Request{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
				var err error

				request.Sources = []SourceUnit{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
				errs, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if tc.conflict {
					want = 1
				}
				if len(errs) != want || (want == 1 && !strings.Contains(errs[0].Message, "cannot be combined")) {
					t.Fatalf("want %d conflicts, got %v", want, errs)
				}
			})
		}
	}
}

func TestActionMappingInterpolationReview(t *testing.T) {
	for _, field := range []string{"env", "with"} {
		for _, value := range []string{"prefix-${{ inputs.value }}", "${{ inputs.value }}-suffix", "${{ inputs.value }}tail}}", "${{ inputs.value }}${{ inputs.value }}", "${{ fromJSON(inputs.value) }}", `${{ fromJSON('{"key":"${{"}') }}`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				var meta actionmeta.Metadata
				source := `name: test
description: test
inputs:
  value:
    description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      ` + field + ": " + value + "\n"
				if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
					t.Fatal(err)
				}
				findings := actionmeta.Validate(&meta)
				want := 1
				if strings.HasPrefix(value, "${{ fromJSON(") {
					want = 0
				}
				if len(findings) != want || (want == 1 && !strings.Contains(findings[0].Message, "expected a mapping")) {
					t.Fatalf("want %d mapping errors, got %v", want, findings)
				}
			})
		}
	}
}

func TestCheckStringRejectsRawTagReview(t *testing.T) {
	for _, allowEmpty := range []bool{false, true} {
		p := &workflowlang.Parser{}
		n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!custom", Value: "max", Style: yaml.TaggedStyle, Line: 1, Column: 1}
		if p.CheckString(n, allowEmpty) {
			t.Fatal("invalid tag accepted")
		}
	}
}

func TestWorkflowExpressionLiteralBoundaryReview(t *testing.T) {
	for _, value := range []string{"pre-${{ '' }}", "${{ '' }}-post", "${{ '' }}-${{ '' }}", "${{ '' }}tail}}", "  ${{ '' }}  ", "${{ '' }}\n", "\t${{ '' }}"} {
		if got, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: value}); known {
			t.Errorf("fragment %q folded to %#v", value, got)
		}
	}
	if got, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: "${{  ''  }}"}); !known || got != "" {
		t.Fatalf("sole expression: %#v, known=%v", got, known)
	}
}
