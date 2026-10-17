package suppression_test

import (
	"testing"

	"actionlint.kjanat.dev/internal/testutil"
)

func TestGeneralInlineSuppression(t *testing.T) {
	for _, tc := range []struct{ body, config string }{
		{"    if: true # actionlint:ignore if-cond -- intentionally always runs\n", ""},
		{`    # actionlint:ignore-next-line expression-reference -- provided by invocation
    if: typo.value
`, ""},
		{"    if: github.ref == 'refs/heads/main' # actionlint:ignore case-insensitive-conditions -- casing intentionally irrelevant\n", "lint: {rules: {suspicious: {case-insensitive-conditions: on}}}"},
	} {
		source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
` + tc.body + `    steps:
      - run: echo ok
`
		if got := testutil.WorkflowDiagnostics(t, source, tc.config); len(got) != 0 {
			t.Fatal(got)
		}
	}
	if got := testutil.WorkflowDiagnostics(t, "jobs: [\n", "lint: {rules: {disable: [syntax-check]}}"); len(got) != 0 {
		t.Fatal(got)
	}
}
