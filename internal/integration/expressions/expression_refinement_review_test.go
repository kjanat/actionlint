package expressions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
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
		t.Run(tc.name, func(t *testing.T) { checkJobField(t, tc.field, tc.want) })
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
				request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
				var requestSources []workspace.Source
				var err error
				requestSources = []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Project: nil}}
				errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
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
