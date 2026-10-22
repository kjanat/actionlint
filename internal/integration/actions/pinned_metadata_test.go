package actions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestPinnedActionInputsAndOutputs(t *testing.T) {
	const pinned = "11d5960a326750d5838078e36cf38b85af677262"
	for _, tc := range []struct {
		ref, input, output      string
		inputError, outputError bool
	}{
		{pinned, "fetch-depth", "commit", false, false},
		{pinned, "misspelled-input", "misspelled_output", true, true},
		{"0000000000000000000000000000000000000000", "misspelled-input", "misspelled_output", false, false},
	} {
		source := fmt.Sprintf(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@%s
        id: checkout
        with:
          %s: 1
      - run: echo ok
        env:
          VALUE: ${{ steps.checkout.outputs.%s }}
`, tc.ref, tc.input, tc.output)
		findings := testutil.WorkflowDiagnostics(t, source, "tools: {shellcheck: false, ruff: false}")
		inputError, outputError := false, false
		for _, d := range findings {
			inputError = inputError || strings.Contains(d.Message, "misspelled-input")
			outputError = outputError || strings.Contains(d.Message, "misspelled_output")
		}
		if inputError != tc.inputError || outputError != tc.outputError {
			t.Fatalf("%+v: %+v", tc, findings)
		}
	}
}
