package expressions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestCompositeNameAndScalarDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, script, rule, suggestion string
	}{
		{"context", "echo '${{ githbu.ref }}'", "action", "github"},
		{"function", "echo '${{ startWith(github.ref, 'refs/') }}'", "action", "startsWith"},
		{"boolean command", "${{ true }}", "script-interpolation", ""},
		{"number command", "${{ 42 }}", "script-interpolation", ""},
		{"serialized null command", "${{ toJSON(null) }}", "script-interpolation", ""},
		{"scalar argument", "echo '${{ true }}' '${{ 42 }}' '${{ toJSON(null) }}'", "", ""},
		{"unknown input", "echo '${{ inputs.text }}'", "script-interpolation", ""},
		{"unknown output", "echo '${{ steps.build.outputs.text }}'", "script-interpolation", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			testutil.WriteFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        "+tc.script+"\n")
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if tc.rule == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.rule {
				t.Fatalf("expected one %s finding: %+v", tc.rule, result.Diagnostics)
			}
			if tc.suggestion != "" && !strings.Contains(result.Diagnostics[0].Message, `did you mean "`+tc.suggestion+`"?`) {
				t.Fatal(result.Diagnostics)
			}
		})
	}
}

func TestCompositeInterpolationIndependentOfActionValidation(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		for _, level := range []string{"error", "off"} {
			t.Run(fmt.Sprintf("standalone=%t/%s", standalone, level), func(t *testing.T) {
				root, _ := testutil.ExecutableFixture(t)
				config := "tools: {shellcheck: false, ruff: false}\nlint: {rules: {correctness: {action: off, expression: off, script-interpolation: " + level + "}}}\n"
				body := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ${{ true }}\n"
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
				testutil.WriteFixture(t, root, "inner/action.yml", body)
				result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
				if standalone {
					cfg, err := repoconfig.Parse([]byte(config))
					if err != nil {
						t.Fatal(err)
					}
					result, err = application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: "inner/action.yml", Content: []byte(body), Config: cfg}})
					if err != nil {
						t.Fatal(err)
					}
				}
				want := 1
				if level == "off" {
					want = 0
				}
				if len(result.Diagnostics) != want || want == 1 && result.Diagnostics[0].Rule != "script-interpolation" {
					t.Fatalf("action and expression settings must not change the independent script rule: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeInvalidNamePreservesIndependentScriptFinding(t *testing.T) {
	for _, script := range []string{
		"echo '${{ github.ref_name }}' '${{ githbu.ref }}'",
		"echo '${{ githbu.ref }}' '${{ github.ref_name }}'",
	} {
		root, _ := testutil.ExecutableFixture(t)
		testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
		testutil.WriteFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: |\n        "+script+"\n")
		result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
		counts := map[string]int{}
		for _, finding := range result.Diagnostics {
			counts[finding.Rule]++
		}
		if len(result.Diagnostics) != 2 || counts["action"] != 1 || counts["script-interpolation"] != 1 {
			t.Fatalf("independent script finding lost or metadata error duplicated: %+v", result.Diagnostics)
		}
	}
}
