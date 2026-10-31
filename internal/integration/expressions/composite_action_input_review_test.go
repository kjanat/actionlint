package expressions_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestCompositeActionInputScriptSinks(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, tc := range []struct {
			name, action, input, value string
			rules                      map[string]int
		}{
			{"untrusted script", "actions/github-script@v8", "script", "console.log('${{ github.event.issue.title }}')", map[string]int{"script-injection": 1}},
			{"unknown input", "actions/github-script@v8", "script", "console.log('${{ inputs.token }}')", map[string]int{"script-interpolation": 1}},
			{"scalar JavaScript", "actions/github-script@v8", "script", "const value = ${{ true }}", map[string]int{"script-interpolation": 1}},
			{"ordinary token input", "actions/github-script@v8", "github-token", "${{ inputs.token }}", nil},
			{"ordinary action", "example/action@v1", "script", "${{ github.event.issue.title }}", nil},
			{"invalid context", "actions/github-script@v8", "script", "console.log('${{ githbu.ref }}')", map[string]int{"action": 1}},
			{"unavailable secrets", "actions/github-script@v8", "script", "console.log('${{ secrets.TOKEN }}')", map[string]int{"action": 1}},
			{"independent invalid context", "actions/github-script@v8", "script", "console.log('${{ githbu.ref }}', '${{ github.event.issue.title }}')", map[string]int{"action": 1, "script-injection": 1}},
		} {
			t.Run(fmt.Sprintf("nested=%t/%s", nested, tc.name), func(t *testing.T) {
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
				body := "name: inner\ndescription: test\ninputs:\n  token:\n    description: test\nruns:\n  using: composite\n  steps:\n    - uses: " + tc.action + "\n      with:\n        " + tc.input + ": |\n          " + tc.value + "\n"
				if tc.input == "github-token" {
					body += "        script: console.log(1)\n"
				}
				testutil.WriteFixture(t, root, "inner/action.yml", body)
				call := "./inner"
				if nested {
					testutil.WriteFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n      with:\n        token: ${{ inputs.token }}\ninputs:\n  token:\n    description: test\n")
					call = "./outer"
				}
				result := testutil.Composite(t, root, "- uses: "+call+"\n  with:\n    token: ${{ secrets.TOKEN }}", application.Options{})
				counts := map[string]int{}
				for _, finding := range result.Diagnostics {
					counts[finding.Rule]++
					if filepath.ToSlash(finding.Path) != "inner/action.yml" || finding.Start.Line != 12 {
						t.Fatalf("finding must point into composite script scalar: %+v", finding)
					}
				}
				for rule, count := range tc.rules {
					if counts[rule] != count {
						t.Fatalf("want %v; got %+v", tc.rules, result.Diagnostics)
					}
				}
				if len(counts) != len(tc.rules) {
					t.Fatalf("unexpected or duplicate diagnostics; want %v: %+v", tc.rules, result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeActionInputScriptSourceLocations(t *testing.T) {
	for _, tc := range []struct {
		name, scalar string
		line, column int
	}{
		{"literal", "|\n          const repo = ${{ github.repository }}", 9, 28},
		{"folded", ">-\n          const repo =\n          ${{ github.repository }}", 10, 15},
		{"quoted newline", `"const repo =\n${{ github.repository }}"`, 8, 36},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+fmt.Sprintf("/%q", ending), func(t *testing.T) {
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
				body := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/github-script@v8\n      with:\n        script: " + tc.scalar + "\n"
				testutil.WriteFixture(t, root, "inner/action.yml", strings.ReplaceAll(body, "\n", ending))
				result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
				if len(result.Diagnostics) != 1 {
					t.Fatalf("one located script finding expected: %+v", result.Diagnostics)
				}
				finding := result.Diagnostics[0]
				if finding.Rule != "script-interpolation" || filepath.ToSlash(finding.Path) != "inner/action.yml" || finding.Start.Line != tc.line || finding.Start.Column != tc.column {
					t.Fatalf("want inner/action.yml:%d:%d; got %+v", tc.line, tc.column, finding)
				}
			})
		}
	}
}

func TestCompositeActionInputMappingValidationOwnership(t *testing.T) {
	for _, tc := range []struct{ name, mapping string }{
		{"whole string", "${{ 'not-a-map' }}"},
		{"whole array", "${{ fromJSON('[]') }}"},
		{"inserted array", "\n        ${{ insert }}: ${{ fromJSON('[]') }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			body := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: example/action@v1\n      with: " + tc.mapping + "\n"
			testutil.WriteFixture(t, root, "inner/action.yml", body)
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" {
				t.Fatalf("metadata must exclusively own malformed composite input mappings: %+v", result.Diagnostics)
			}
			findings := testutil.WorkflowDiagnostics(t, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n        with: "+strings.ReplaceAll(tc.mapping, "\n", "\n  ")+"\n", "tools: {shellcheck: false, ruff: false}\n")
			if len(findings) != 1 || findings[0].Rule != "expression" || !strings.Contains(findings[0].Message, "with must be object") {
				t.Fatalf("workflow input mapping validation must remain: %+v", findings)
			}
		})
	}
}
