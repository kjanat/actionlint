package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestEscapedLineBreakEOFDiagnostics(t *testing.T) {
	t.Parallel()
	for _, escape := range []string{`\r`, `\n`, `\r\n`, `\n\r`, `\x0d`, `\u000d`, `\U0000000d`, `\x0a`, `\u000a`} {
		for _, newline := range []string{"\n", "\r\n"} {
			for _, metadata := range []bool{false, true} {
				t.Run(escape+newline+map[bool]string{true: "metadata", false: "workflow"}[metadata], func(t *testing.T) {
					t.Parallel()
					root, _ := testutil.ExecutableFixture(t, "shellcheck")
					fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
					scalar := `"echo ${{ github.ref` + escape + `"`
					src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + scalar + "\n" + "\npermissions: {}\n"
					if metadata {
						src = "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: " + scalar + "\n"
					}
					src = strings.ReplaceAll(src, "\n", newline)
					var result *checking.Result
					if metadata {
						fixturefiles.Write(t, root, "inner/action.yml", src)
						result = testutil.Composite(t, root, "- uses: ./inner", checking.Options{})
					} else {
						var err error
						result, err = checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
						if err != nil {
							t.Fatal(err)
						}
					}
					if len(result.Diagnostics) != 1 {
						t.Fatalf("expected one EOF diagnostic: %+v", result.Diagnostics)
					}
					eof := strings.LastIndex(src, `"`)
					assertDiagnosticOffsetRange(t, src, result.Diagnostics[0], eof, eof)
				})
			}
		}
	}
}
