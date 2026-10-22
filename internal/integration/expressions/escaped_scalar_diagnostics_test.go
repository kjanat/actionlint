package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestEscapedLineBreakEOFDiagnostics(t *testing.T) {
	for _, escape := range []string{`\r`, `\n`, `\r\n`, `\n\r`, `\x0d`, `\u000d`, `\U0000000d`, `\x0a`, `\u000a`} {
		for _, newline := range []string{"\n", "\r\n"} {
			for _, metadata := range []bool{false, true} {
				t.Run(escape+newline+map[bool]string{true: "metadata", false: "workflow"}[metadata], func(t *testing.T) {
					root, _ := testutil.ExecutableFixture(t)
					testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
					scalar := `"echo ${{ github.ref` + escape + `"`
					src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + scalar + "\n"
					if metadata {
						src = "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: " + scalar + "\n"
					}
					src = strings.ReplaceAll(src, "\n", newline)
					var result *application.Result
					if metadata {
						testutil.WriteFixture(t, root, "inner/action.yml", src)
						result = testutil.Composite(t, root, "- uses: ./inner", application.Options{})
					} else {
						var err error
						result, err = application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: "ci.yml", Content: []byte(src)}})
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
