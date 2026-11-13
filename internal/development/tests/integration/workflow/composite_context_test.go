package workflow_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
)

func TestCompositeUnavailableContextsDoNotDuplicatePropertyFindings(t *testing.T) {
	t.Parallel()
	const expression = "${{ format('{0}:{1}:{2}:{3}:{4}', inputs.token, needs.build.result, secrets.GITHUB_TOKEN, vars.REGION, steps.child.outputs.missing) }}"
	for _, outputValue := range []bool{false, true} {
		for _, folded := range []bool{false, true} {
			t.Run(fmt.Sprintf("output-value=%t/folded=%t", outputValue, folded), func(t *testing.T) {
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
				fixturefiles.Write(t, root, "child/action.yml", "name: child\ndescription: child\noutputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n")
				content := "name: outer\ndescription: outer\ninputs: {token: {description: token}}\n"
				if outputValue {
					content += "outputs:\n  result:\n    description: result\n    value: "
					if folded {
						content += ">-\n      prefix\n      "
					}
					content += expression + "\n"
				}
				content += "runs:\n  using: composite\n  steps:\n    - uses: $/child\n      id: child\n"
				if !outputValue {
					content += "    - run: echo ready\n      shell: bash\n      env:\n        VALUE: "
					if folded {
						content += ">-\n          prefix\n          "
					}
					content += expression + "\n"
				}
				entry := fixturefiles.Write(t, root, "outer/action.yml", content)
				result, err := checking.Check(t.Context(), checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{entry}})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 4 {
					t.Fatalf("expected three unavailable-context findings and one missing output: %+v", result.Diagnostics)
				}
				unavailable := map[string]bool{"needs": false, "secrets": false, "vars": false}
				properties := 0
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule == "action" {
						for name := range unavailable {
							if strings.Contains(diagnostic.Message, "context \""+name+"\"") {
								unavailable[name] = true
							}
						}
						continue
					}
					properties++
					if diagnostic.Rule != "expression-reference" || diagnostic.Code != "unknown-property" || !strings.Contains(diagnostic.Message, "steps.child.outputs") || !strings.HasSuffix(filepath.ToSlash(diagnostic.Path), "outer/action.yml") {
						t.Fatalf("unexpected secondary finding: %+v", diagnostic)
					}
					if diagnostic.End.Line != diagnostic.Start.Line || diagnostic.End.Column <= diagnostic.Start.Column {
						t.Fatalf("missing output has no source range: %+v", diagnostic)
					}
					line := []rune(strings.Split(content, "\n")[diagnostic.Start.Line-1])
					if got := string(line[diagnostic.Start.Column-1 : diagnostic.End.Column-1]); got != "steps.child.outputs.missing" {
						t.Fatalf("missing output range selected %q: %+v", got, diagnostic)
					}
				}
				for name, found := range unavailable {
					if !found {
						t.Errorf("missing availability finding for %s", name)
					}
				}
				if properties != 1 {
					t.Fatalf("missing output findings=%d, want one", properties)
				}
			})
		}
	}
}
