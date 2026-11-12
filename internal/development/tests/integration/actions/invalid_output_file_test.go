package actions_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestInvalidOutputFileStandaloneAndCalledComposite(t *testing.T) {
	t.Parallel()
	// Test only the output-writing step from PR comment 6710252262.
	// The runner rejects the first line, `{`, as an invalid output record.
	const action = `name: Output format regression
description: Check a named output file
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        tee <<EOF2 >"${GITHUB_OUTPUT}"
        {
          "available": true
        }
        EOF2
`
	for _, called := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "called"}[called], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			var result *checking.Result
			if called {
				root, _ = testutil.ExecutableFixture(t, "shellcheck", "ruff")
				fixturefiles.Write(t, root, "inner/action.yml", action)
				result = testutil.Composite(t, root, "- uses: ./inner", checking.Options{})
			} else {
				var err error
				result, err = checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: "action.yml", Content: []byte(action)}})
				if err != nil {
					t.Fatal(err)
				}
			}
			var matches []results.Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "invalid-output-file" {
					matches = append(matches, diagnostic)
				}
			}
			if len(matches) != 1 || matches[0].Start.Line != 8 || matches[0].Start.Column != 21 || !strings.HasSuffix(filepath.ToSlash(matches[0].Path), "action.yml") {
				t.Fatalf("expected producer destination finding at 8:21: %+v (all: %+v)", matches, result.Diagnostics)
			}
		})
	}
}
