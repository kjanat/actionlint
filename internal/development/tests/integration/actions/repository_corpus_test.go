package actions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestRepositoryActionMetadata(t *testing.T) {
	t.Parallel()
	options := checking.Options{RemoteActions: testutil.FixtureActions(t)}
	for _, tc := range []struct {
		ref, inputs, output string
		closedOutputs       bool
	}{
		{"kjanat/actions-shells@master", "doctor: true", "cli", false},
		{"kjanat/install-dprint@master", "warmup: false", "plugin-cache-key", false},
		{"kjanat/actionlint-registry/publish@master", "sha256: digest, candidate-receipt: receipt.json, actionlint: actionlint, fixture: workflow.yml", "submission-id", true},
		{"kjanat/beepboop/actions/credentials@master", "aliases: '[]'", "values", true},
		{"kjanat/svg-converter-action@master", "svg-path: drawing.svg", "files-created", false},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			for _, mutation := range []string{"valid", "input", "output"} {
				t.Run(mutation, func(t *testing.T) {
					t.Parallel()
					inputs, output := tc.inputs, tc.output
					switch mutation {
					case "input":
						inputs += ", misspelled-input: value"
					case "output":
						output = "misspelled-output"
					}
					source := fmt.Sprintf(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: %s
        id: installed
        with: {%s}
      - run: echo ok
        env:
          RESULT: ${{ steps.installed.outputs.%s }}
permissions: {}
`, tc.ref, inputs, output)
					findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "repository: {runners: []}\ntools: {shellcheck: false, ruff: false}", options)
					if mutation == "valid" || mutation == "output" && !tc.closedOutputs {
						if len(findings) != 0 {
							t.Fatalf("valid action invocation rejected: %v", findings)
						}
						return
					}
					want, rule, line := "misspelled-input", "action", 8
					if mutation == "output" {
						want, rule, line = "misspelled-output", "expression-reference", 11
					}
					if len(findings) != 1 || findings[0].Rule != rule || findings[0].Start.Line != line || !strings.Contains(findings[0].Message, want) {
						t.Fatalf("want one %s finding for %q on line %d, got %v", rule, want, line, findings)
					}
				})
			}
		})
	}
}

func TestCommitishNestedActionOrigins(t *testing.T) {
	t.Parallel()
	fixtureDirectory := filepath.Join("internal", "integration", "actions", "testdata", "commitish")
	if _, err := os.Stat(fixtureDirectory); os.IsNotExist(err) {
		t.Skip("local Commitish fixtures are unavailable")
	}
	for _, spelling := range []string{"dot", "dollar"} {
		for _, checkout := range []string{"self", "foreign"} {
			t.Run(spelling+"/"+checkout, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				if err := os.CopyFS(root, os.DirFS(fixtureDirectory)); err != nil {
					t.Fatal(err)
				}
				config, err := checking.ConfigurationRules().Parse([]byte("repository: {runners: []}\ntools: {shellcheck: false, ruff: false}"))
				if err != nil {
					t.Fatal(err)
				}
				checkoutInputs := ""
				if checkout == "foreign" {
					checkoutInputs = "\n        with: {repository: other/repository}"
				}
				source := fmt.Sprintf(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7%s
      - uses: $/nesting/%s
permissions: {}
`, checkoutInputs, spelling)
				result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{
					Path: ".github/workflows/caller.yml", Content: []byte(source), Project: workspace.Identity(root), Config: config,
				}})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 0 {
					t.Fatalf("nested fixture rejected: %v", result.Diagnostics)
				}
				parent := filepath.Join(root, "nesting", spelling, "action.yml")
				if !slices.Contains(result.Inputs, parent) {
					t.Fatalf("parent source was not analyzed: %v", result.Inputs)
				}
				child := filepath.Join(root, "child", "action.yml")
				wantChild := spelling == "dollar" || checkout == "self"
				if slices.Contains(result.Inputs, child) != wantChild {
					t.Fatalf("source child read=%v, want %v: %v", slices.Contains(result.Inputs, child), wantChild, result.Inputs)
				}
				shadow := filepath.Join(root, "nesting", spelling, "child", "action.yml")
				if slices.Contains(result.Inputs, shadow) {
					t.Fatalf("nested action incorrectly resolved relative to its own directory: %v", result.Inputs)
				}
			})
		}
	}
}
