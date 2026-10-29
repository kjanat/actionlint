package workflow_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

// case() results are typed by merging every value branch with the default, so
// a workflow using a string-valued case() where a number is required is
// reported, while number-valued and unknown-valued ones pass.
func TestCaseResultTypeInference(t *testing.T) {
	for _, tc := range []struct {
		name, timeout, want string
	}{
		{"string results", "case(github.ref == 'refs/heads/main', 'production', 'development')", "must be number but found type string"},
		{"number results", "case(github.ref == 'refs/heads/main', 10, 5)", ""},
		{"nested string results", "case(github.ref == 'refs/heads/main', case(github.ref == 'refs/heads/dev', 'a', 'b'), 'c')", "must be number but found type string"},
		{"object property", `case(github.ref == 'refs/heads/main', fromJSON('{"timeout":10}'), fromJSON('{"timeout":5}')).timeout`, ""},
		{"missing object property", `case(github.ref == 'refs/heads/main', fromJSON('{"timeout":10}'), fromJSON('{"timeout":5}')).missing`, `property "missing" is not defined`},
		{"unknown default", "case(github.ref == 'refs/heads/main', 10, fromJSON(vars.TIMEOUT))", ""},
		{"constant predicate narrows to reachable branch", "case(true, 10, 'unreachable')", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := application.Options{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
			src := `on: push
env:
  MY_ENV_VAR: |-
    ${{ case(
      github.ref == 'refs/heads/main', 'production',
      github.ref == 'refs/heads/staging', 'staging',
      startsWith(github.ref, 'refs/heads/feature/'), 'development',
      'unknown'
    ) }}
jobs:
  test:
    runs-on: ubuntu-latest
    timeout-minutes: ${{ ` + tc.timeout + ` }}
    steps:
      - run: echo "$MY_ENV_VAR"
`
			sources := []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(src), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, sources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %v", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted one diagnostic containing %q, got %v", tc.want, errs)
			}
		})
	}
}
