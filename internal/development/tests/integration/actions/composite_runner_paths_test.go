package actions_test

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
)

func TestCompositeExecutableUnixPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, checkout, directory, script string
	}{
		{"colon checkout", "a:debug", "${{ github.action_path }}", "./bad.sh"},
		{"colon directory", "a:debug", "a:debug/local", "./bad.sh"},
		{"colon cd", "a:debug", ".", "cd a:debug/local && ./bad.sh"},
		{"colon direct operand", "a:debug", ".", "./a:debug/local/bad.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, git := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
			git("add", "local/bad.sh")
			git("update-index", "--chmod=-x", "local/bad.sh")
			metadata := fixturefiles.Write(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '`+tc.directory+"'\n      run: "+tc.script+"\n")
			result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n  with: {path: '"+tc.checkout+"'}\n- uses: ./"+tc.checkout+"/local", checking.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata
			})
			if found != (runtime.GOOS != "windows") {
				t.Fatalf("wrong Unix checkout script result: %+v", result.Diagnostics)
			}
		})
	}
}
