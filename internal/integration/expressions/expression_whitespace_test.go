package expressions_test

import (
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestExpressionWhitespaceWorkflowBoundaries(t *testing.T) {
	// TemplateReader preserves whitespace outside expression delimiters as text.
	// https://github.com/actions/runner/blob/fee24199cba8acf6c25da3a061c986066f42327a/src/Sdk/DTObjectTemplating/ObjectTemplating/TemplateReader.cs#L563-L593
	for _, tc := range []struct{ name, field, message string }{
		{"quoted environment", `environment: "  ${{ '' }}  "`, ""},
		{"block environment", "environment: |\n      ${{ '' }}", ""},
		{"plain environment", "environment:   ${{ '' }}   ", "must be a non-empty string"},
		{"exact environment", "environment: ${{ 'production' }}", ""},
		{"quoted env mapping", `env: " ${{ fromJSON('{}') }} "`, "expecting a single"},
		{"quoted unknown mapping", `env: " ${{ fromJSON(vars.ENVIRONMENT) }} "`, "expecting a single"},
		{"block env mapping", "env: |\n      ${{ fromJSON('{}') }}", "expecting a single"},
		{"plain env mapping", "env:   ${{ fromJSON('{}') }}   ", ""},
		{"stripped block env mapping", "env: |-\n      ${{ fromJSON('{}') }}", ""},
		{"bare if whitespace", `if: "  github.ref == 'refs/heads/main'  "`, ""},
		{"interpolated if", `if: "  ${{ false }}  "`, "always evaluated to true because extra characters"},
	} {
		t.Run(tc.name, func(t *testing.T) { checkJobField(t, tc.field, tc.message) })
	}
}

func TestExpressionWhitespaceStaticUses(t *testing.T) {
	for _, tc := range []struct {
		name, value, parsed string
		invalid             bool
	}{
		{"plain", "  ${{ './action' }}  ", "./action", false},
		{"quoted", `"  ${{ './action' }}  "`, "  ${{ './action' }}  ", true},
		{"block", "|\n          ${{ './action' }}", "${{ './action' }}\n", true},
		{"stripped block", "|-\n          ${{ './action' }}", "./action", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(`on: push
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: ` + tc.value + "\n")
			workflow, errs := workflowparser.ParseDocument(source)
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			uses := workflow.Jobs["check"].Steps[0].Exec.(*workflowlang.ExecAction).Uses
			if uses.Value != tc.parsed {
				t.Fatalf("decoded uses changed: got %q, want %q", uses.Value, tc.parsed)
			}
			request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
			var requestSources []workspace.Source
			var err error
			requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: source, Project: nil}}
			errs, err = testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				if len(errs) != 1 || !strings.Contains(errs[0].Message, "expressions are not allowed in this static workflow field") {
					t.Fatalf("want static interpolation diagnostic, got %v", errs)
				}
			} else if len(errs) != 0 {
				t.Fatalf("valid static reference rejected: %v", errs)
			}
		})
	}
}

func TestExpressionWhitespacePreservesExpressionErrors(t *testing.T) {
	for _, value := range []string{"${{ ... }}", "${{ fromJSON( }}", "${{ 'unfinished }}"} {
		t.Run(value, func(t *testing.T) {
			request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
			var requestSources []workspace.Source
			var err error

			source := `on: push
jobs:
  check:
    runs-on: ubuntu-latest
    env: "` + value + `"
    steps:
      - run: echo ok
`
			requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: nil}}
			errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err != nil {
				t.Fatal(err)
			}
			if len(errs) != 1 || errs[0].Rule != "expression-syntax" {
				t.Fatalf("want one expression syntax diagnostic, got %v", errs)
			}
		})
	}
}

// checkJobField analyzes a one-job workflow carrying field and expects no
// diagnostic when want is empty, otherwise exactly one containing want.
func checkJobField(t *testing.T, field, want string) {
	t.Helper()
	source := `on: push
jobs:
  check:
    runs-on: ubuntu-latest
    ` + field + `
    steps:
      - run: echo ok
`
	request := application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
	sources := []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source)}}
	errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, sources))
	if err != nil {
		t.Fatal(err)
	}
	if want == "" {
		if len(errs) != 0 {
			t.Fatalf("unexpected diagnostics: %v", errs)
		}
	} else if len(errs) != 1 || !strings.Contains(errs[0].Message, want) {
		t.Fatalf("want one %q diagnostic, got %v", want, errs)
	}
}
