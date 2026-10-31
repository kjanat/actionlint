package actions_test

import (
	"path/filepath"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestActionMetadataContextSuggestion(t *testing.T) {
	const source = `name: test
description: test
runs:
  using: composite
  steps:
    - run: echo '${{ github_repository }}'
      shell: bash
`
	meta, err := actionmeta.Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	findings := actioncheck.Check(meta, &workflowlang.Pos{Line: 1, Col: 1})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, `did you mean "github.repository"?`) || findings[0].Path != "action.yml" || findings[0].Start.Line != 6 {
		t.Fatalf("metadata suggestion lost diagnostic or position: %v", findings)
	}
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	testutil.WriteFixture(t, root, "inner/action.yml", source)
	testutil.WriteFixture(t, root, "outer/action.yml", "name: outer\ndescription: outer\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
	result := testutil.Composite(t, root, "- uses: ./outer", application.Options{})
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
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			testutil.WriteFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\ninputs:\n  github_repository:\n    description: repository\nruns:\n  using: composite\n  steps:\n    - run: echo '"+tc.expression+"'\n      shell: bash\n")
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if tc.want == "" {
				if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-interpolation" || result.Diagnostics[0].Code != "unknown-script-value" {
					t.Fatalf("qualified input must retain its independent script finding: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, tc.want) {
				t.Fatalf("want %s, got %+v", tc.want, result.Diagnostics)
			}
		})
	}
	// Inputs cannot be referenced while evaluating their defaults.
	violations := actionmeta.ExpressionViolations("${{ github_repository }}", false, "inputs.*.default", actionmeta.Inputs{"github_repository": {Name: "github_repository"}})
	if len(violations) != 1 || !strings.Contains(violations[0].Message, `did you mean "github.repository"?`) {
		t.Fatalf("suggested unavailable input context: %+v", violations)
	}
}

func TestCompositeInjectionSurvivesExpressionError(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	testutil.WriteFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - run: echo "${{ github.event.issue.title || github.events }}"
      shell: bash
`)
	result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule == "script-injection" && strings.Contains(diagnostic.Message, "potentially untrusted") {
			return
		}
	}
	t.Fatalf("missing independent composite injection finding: %+v", result.Diagnostics)
}
