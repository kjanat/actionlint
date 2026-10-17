package engine

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/tools/adapters"
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
