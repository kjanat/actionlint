package actions_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
)

func TestCompositeStatusConditionCheckout(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`)
			result := testutil.Composite(t, root, "- uses: ./local\n  if: ${{ "+tc.condition+` }}
- uses: actions/checkout@v6
- shell: bash
  working-directory: .
  run: ./bad.sh`, checking.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}
