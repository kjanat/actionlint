package actionlint

import (
	"slices"
	"testing"
)

func TestCompositeStatusConditionCheckout(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n")
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
	} {
		t.Run(tc.condition, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: ./local\n  if: ${{ "+tc.condition+" }}\n- uses: actions/checkout@v6\n- shell: bash\n  working-directory: .\n  run: ./bad.sh", AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}
