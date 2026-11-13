package actions_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

func TestVendorLockInCompositeSources(t *testing.T) {
	t.Parallel()
	root, _ := testutil.ExecutableFixture(t, "shellcheck", "ruff")
	fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, correctness: {vendor-lock-in: on}}}\n")
	const content = "name: wrapper\ndescription: wrapper\nruns:\n  using: composite\n  steps:\n    - uses: https://github.com/mlugg/setup-zig@v2\n"
	action := fixturefiles.Write(t, root, "wrapper/action.yml", content)
	workflow := fixturefiles.Write(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/wrapper\npermissions: {}\n")
	for _, entry := range []string{action, workflow} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{entry}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("findings=%+v", result.Diagnostics)
			}
			finding := result.Diagnostics[0]
			if finding.Rule != "vendor-lock-in" || !strings.HasSuffix(filepath.ToSlash(finding.Path), "wrapper/action.yml") || finding.Start.Line != 6 || len(finding.Fixes) != 1 {
				t.Fatalf("wrong declaration or fix: %+v", finding)
			}
		})
	}
}
