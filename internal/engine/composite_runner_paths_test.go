package engine

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
)

func TestCompositeExecutableUnixPaths(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, tc := range []struct {
		name, checkout, directory, script string
	}{
		{"colon checkout", "a:debug", "${{ github.action_path }}", "./bad.sh"},
		{"colon directory", "a:debug", "a:debug/local", "./bad.sh"},
		{"colon cd", "a:debug", ".", "cd a:debug/local && ./bad.sh"},
		{"colon direct operand", "a:debug", ".", "./a:debug/local/bad.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '`+tc.directory+"'\n      run: "+tc.script+"\n")
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: '"+tc.checkout+"'}\n- uses: ./"+tc.checkout+"/local", Request{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata
			})
			if found != (runtime.GOOS != "windows") {
				t.Fatalf("wrong Unix checkout script result: %+v", result.Diagnostics)
			}
		})
	}
}
