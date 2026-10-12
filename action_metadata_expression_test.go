package actionlint

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v4"
)

func TestActionExpressionContextSuggestions(t *testing.T) {
	for _, tc := range []struct {
		field, expression string
		want              []actionExpressionViolation
	}{
		{"runs.steps.*.run", "${{ github_repository }}", []actionExpressionViolation{{message: `uses undefined variable "github_repository"; did you mean "github.repository"?`, offset: 5}}},
		{"runs.steps.*.if", "GITHUB_REPOSITORY", []actionExpressionViolation{{message: `uses undefined variable "GITHUB_REPOSITORY"; did you mean "github.repository"?`, offset: 1}}},
		{"inputs.*.default", "${{ runner_os }}", []actionExpressionViolation{{message: `uses undefined variable "runner_os"; did you mean "runner.os"?`, offset: 5}}},
		{"runs.args.*", "${{ github_repository }}", []actionExpressionViolation{{context: "github_repository"}}},
		{"runs.steps.*.run", "${{ secrets.TOKEN }}", []actionExpressionViolation{{context: "secrets"}}},
		{"runs.steps.*.run", "${{ github_nonexistent }}", []actionExpressionViolation{{context: "github_nonexistent"}}},
		{"runs.steps.*.run", "${{ inputs.github_token }} ${{ matrix.runner_os }}", nil},
	} {
		t.Run(tc.field+"/"+tc.expression, func(t *testing.T) {
			got := actionExpressionViolations(tc.expression, tc.field == "runs.steps.*.if", tc.field)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(actionExpressionViolation{})); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestActionMetadataContextSuggestion(t *testing.T) {
	const source = `name: test
description: test
runs:
  using: composite
  steps:
    - run: echo '${{ github_repository }}'
      shell: bash
`
	var meta ActionMetadata
	if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
		t.Fatal(err)
	}
	meta.file, meta.src = "action.yml", []byte(source)
	rule := NewRuleAction(nil)
	rule.checkLocalActionMetadata(&meta, &ExecAction{Uses: &String{Value: "./test", Pos: &Pos{Line: 1, Col: 1}}})
	findings := rule.Errs()
	if len(findings) != 1 || !strings.Contains(findings[0].Message, `did you mean "github.repository"?`) || findings[0].Filepath != "action.yml" || findings[0].Line != 6 {
		t.Fatalf("metadata suggestion lost diagnostic or position: %v", findings)
	}
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "inner/action.yml", source)
	writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: outer\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	result := compositeAnalysis(t, root, "- uses: ./outer", AnalysisOptions{})
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, `did you mean "github.repository"?`) || filepath.ToSlash(result.Diagnostics[0].Path) != "inner/action.yml" || result.Diagnostics[0].Start.Line != 6 {
		t.Fatalf("nested composite suggestion: %+v", result.Diagnostics)
	}
}

func TestActionMetadataDeclaredInputSuggestion(t *testing.T) {
	for _, tc := range []struct{ expression, want string }{
		{"${{ github_repository }}", `did you mean "inputs.github_repository"?`},
		{"${{ GITHUB_REPOSITORY }}", `did you mean "inputs.github_repository"?`},
		{"${{ inputs.github_repository }}", ""},
		{"${{ runner_os }}", `did you mean "runner.os"?`},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\ninputs:\n  github_repository:\n    description: repository\nruns:\n  using: composite\n  steps:\n    - run: echo '"+tc.expression+"'\n      shell: bash\n")
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			if tc.want == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("qualified input reported: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, tc.want) {
				t.Fatalf("want %s, got %+v", tc.want, result.Diagnostics)
			}
		})
	}
	// Inputs cannot be referenced while evaluating their defaults.
	violations := actionExpressionViolationsWithInputs("${{ github_repository }}", false, "inputs.*.default", ActionMetadataInputs{"github_repository": {Name: "github_repository"}})
	if len(violations) != 1 || !strings.Contains(violations[0].message, `did you mean "github.repository"?`) {
		t.Fatalf("suggested unavailable input context: %+v", violations)
	}
}

func TestActionExpressionFunctions(t *testing.T) {
	tests := []struct {
		name, field, expression string
		bare                    bool
		want                    []actionExpressionViolation
	}{
		{"bare condition", "runs.steps.*.if", "success() && !cancelled()", true, nil},
		{"interpolated condition", "runs.steps.*.if", "${{ always() || failure() }}", true, nil},
		{"status outside condition", "runs.steps.*.name", "${{ success() }}", false,
			[]actionExpressionViolation{{message: `calls function "success" which is not available here`}}},
		{"status in default", "inputs.*.default", "${{ always() }}", false,
			[]actionExpressionViolation{{message: `calls function "always" which is not available here`}}},
		{"hash with no patterns", "inputs.*.default", "${{ hashFiles() }}", false,
			[]actionExpressionViolation{{message: `calls function "hashfiles" with 0 argument(s); expected at least 1 and at most 255`}}},
		{"status with argument", "runs.steps.*.if", "${{ success('x') }}", true,
			[]actionExpressionViolation{{message: `calls function "success" with 1 argument(s); expected at least 0 and at most 0`}}},
		{"case insensitive nested functions", "runs.steps.*.run", "${{ format('{0}', HASHFILES('*.go')) }}", false, nil},
		{"caller properties have no invented types", "runs.steps.*.run", "${{ inputs.custom }} ${{ steps.custom.outputs.custom }}", false, nil},
		{"quoted delimiters", "runs.steps.*.name", "${{ '}} ${{ success() }}' }}", false, nil},
		{"literal then real function", "runs.steps.*.name", "${{ '}} ${{ success() }}' }}${{ success() }}", false,
			[]actionExpressionViolation{{message: `calls function "success" which is not available here`}}},
		{"deduplicated functions", "inputs.*.default", "${{ always() }}${{ ALWAYS() }}", false,
			[]actionExpressionViolation{{message: `calls function "always" which is not available here`}}},
		{"hash maximum", "runs.steps.*.shell", "${{ hashFiles(" + strings.Repeat("'x',", 254) + "'x') }}", false, nil},
		{"hash over maximum", "runs.steps.*.shell", "${{ hashFiles(" + strings.Repeat("'x',", 255) + "'x') }}", false,
			[]actionExpressionViolation{{message: `calls function "hashfiles" with 256 argument(s); expected at least 1 and at most 255`}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, actionExpressionViolations(tc.expression, tc.bare, tc.field), cmp.AllowUnexported(actionExpressionViolation{})); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestActionConditionCompleteConsumption(t *testing.T) {
	for _, tc := range []struct {
		source  string
		invalid bool
	}{
		{"${{ 'github.ref }} ignored' }}", true},
		{"github.ref }} ignored", true},
		{"${{ 'github.ref }}' }}", true},
		{"${{ 'contains(github.ref, ''}}'')' }}", false},
		{"contains(github.ref, '}}')", false},
		{"${{ contains(github.ref, '}}') }}", false},
		{"${{ github.ref }} text ${{ github.actor }}", false},
		{"${{ '' }}", false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			violations := actionExpressionViolations(tc.source, true, "runs.steps.*.if")
			if tc.invalid {
				if len(violations) != 1 || !strings.Contains(violations[0].message, "unexpected trailing text in condition") {
					t.Fatalf("want trailing-text error, got %#v", violations)
				}
			} else if len(violations) != 0 {
				t.Fatalf("unexpected violations: %#v", violations)
			}
		})
	}
}
