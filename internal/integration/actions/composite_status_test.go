package actions_test

import (
	"slices"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/testutil"
)

func TestCompositeStatusConditionCheckout(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
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
			result := testutil.Composite(t, root, "- uses: ./local\n  if: ${{ "+tc.condition+` }}
- uses: actions/checkout@v6
- shell: bash
  working-directory: .
  run: ./bad.sh`, application.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}
