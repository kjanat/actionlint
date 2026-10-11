package actionlint

import (
	"slices"
	"testing"
)

func TestCompositeStatusConditionCheckout(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`)
	for _, tc := range []struct {
		condition string
		want      bool
	}{
		{"always()", false},
		{"failure()", false},
		{"cancelled()", false},
		{"!success()", false},
		{"success()", true},
		{"success() && always()", true},
		{"'always()'", false},
		{"'failure()'", false},
		{"'cancelled()'", false},
		{"'!success()'", false},
		{"'success()'", true},
		{"'success() && always()'", true},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: ./local\n  if: ${{ "+tc.condition+` }}
- uses: actions/checkout@v6
- shell: bash
  working-directory: .
  run: ./bad.sh`, AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}
