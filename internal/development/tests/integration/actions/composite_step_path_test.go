package actions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
)

func TestCompositeDiagnosticStepPaths(t *testing.T) {
	t.Parallel()
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	fixturefiles.Write(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo first\n    - run: echo second\n")
	result := testutil.Composite(t, root, "- uses: ./local", checking.Options{})
	if len(result.Diagnostics) != 2 {
		t.Fatalf("expected two missing-shell findings: %+v", result.Diagnostics)
	}
	for index, finding := range result.Diagnostics {
		want := fmt.Sprintf(`"runs.steps[%d]" in metadata`, index)
		if !strings.HasPrefix(finding.Message, want) {
			t.Fatalf("want %s, got %s", want, finding.Message)
		}
	}
}
