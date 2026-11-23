package tools_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
)

func TestShellcheckCompositeOverrideEnablesCallerDisabledTool(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	for _, tc := range []struct{ name, base, override string }{
		{"tool disabled", "tools: {shellcheck: false}\n", "tools: {shellcheck: true}"},
		{"rule disabled", "lint: {rules: {external: {shellcheck: off}}}\n", "lint: {rules: {external: {shellcheck: on}}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			fixturefiles.Write(t, root, ".git/HEAD", "ref: refs/heads/master\n")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", tc.base+"overrides:\n  - includes: ['local/**']\n    "+tc.override+"\n")
			fixturefiles.Write(t, root, "local/action.yml", "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo $2\n")
			workflow := fixturefiles.Write(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n      - shell: bash\n        run: echo $1\npermissions: {}\n")
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.ToolSelections{"shellcheck": {Command: command}})}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" {
					paths = append(paths, filepath.ToSlash(d.Path))
				}
			}
			if len(paths) != 1 || paths[0] != "local/action.yml" {
				t.Fatalf("ShellCheck findings in %q, want one in local/action.yml", paths)
			}
		})
	}
}
