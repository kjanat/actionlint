package application

import (
	"path/filepath"
	"strconv"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestDependencyPathsDoNotResolveWorkflowOverrides(t *testing.T) {
	for _, dependency := range []string{"local/action.yml", "scripts/lib.sh"} {
		for _, ignore := range []bool{false, true} {
			t.Run(dependency+"/"+strconv.FormatBool(ignore), func(t *testing.T) {
				root := t.TempDir()
				text := `lint:
  rules:
    policy:
      require-job-timeout: {level: on, options: {min-minutes: 5}}
overrides:
  - includes: [local/**, scripts/**]
    lint:
      rules:
        policy:
          require-job-timeout: {level: on, options: {max-minutes: 3}}
`
				if ignore {
					text += "paths: {'**': {ignore: ['dependency finding']}}\n"
				}
				cfg, err := repoconfig.Parse([]byte(text))
				if err != nil {
					t.Fatal(err)
				}
				result, err := runTestAnalysis(t.Context(), analysisRequest{
					WorkingDir: root,
					Sources:    []sourceUnit{{Path: ".github/workflows/ci.yml", Content: []byte(commandGoodWorkflow), Config: cfg, Project: workspace.Identity(root)}},
					OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
						rule := workflowvisit.NewBase("expression-reference", "dependency finding")
						rule.Error(&workflowlang.Pos{Line: 1, Col: 1}, "dependency finding")
						rule.Findings[0].Path = filepath.Join(root, dependency)
						rule.Findings[0].Source = []byte("dependency content")
						return []workflowvisit.Rule{&rule}
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if ignore {
					want = 0
				}
				if len(result.Diagnostics) != want {
					t.Fatalf("got findings %+v, want %d", result.Diagnostics, want)
				}
			})
		}
	}
}

func TestWorkflowOverridesApplyToDependencyDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, lint, severity string }{
		{"disabled workflow", "enabled: false", ""},
		{"disabled rule", "rules: {correctness: {expression-reference: off}}", ""},
		{"warning rule", "rules: {correctness: {expression-reference: warn}}", "warning"},
		{"info rule", "rules: {correctness: {expression-reference: info}}", "info"},
	} {
		for _, dependency := range []string{"local/action.yml", "scripts/lib.sh"} {
			t.Run(tc.name+"/"+dependency, func(t *testing.T) {
				root := t.TempDir()
				cfg, err := repoconfig.Parse([]byte("overrides: [{includes: ['./.github/workflows/**'], lint: {" + tc.lint + "}}]"))
				if err != nil {
					t.Fatal(err)
				}
				result, err := runTestAnalysis(t.Context(), analysisRequest{
					WorkingDir: root,
					Sources:    []sourceUnit{{Path: ".github/workflows/ci.yml", Content: []byte(commandGoodWorkflow), Config: cfg, Project: workspace.Identity(root)}},
					OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
						rule := workflowvisit.NewBase("expression-reference", "dependency finding")
						rule.Error(&workflowlang.Pos{Line: 1, Col: 1}, "dependency finding")
						rule.Findings[0].Path = filepath.Join(root, dependency)
						rule.Findings[0].Source = []byte("dependency content")
						return []workflowvisit.Rule{&rule}
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if tc.severity == "" {
					if len(result.Diagnostics) != 0 {
						t.Fatalf("disabled findings remain: %+v", result.Diagnostics)
					}
				} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != tc.severity {
					t.Fatalf("dependency severity=%+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestWorkflowOverrideControlsCompositeFindings(t *testing.T) {
	for _, tc := range []struct{ lint, severity string }{
		{"rules: {correctness: {action: warn}}", "warning"},
		{"enabled: false", ""},
	} {
		t.Run(tc.lint, func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v5
      with:
        nonexistent-input: value
`)
			cfg, err := repoconfig.Parse([]byte("overrides: [{includes: ['.github/workflows/**'], lint: {" + tc.lint + "}}]"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := runTestAnalysis(t.Context(), analysisRequest{WorkingDir: root, Sources: []sourceUnit{{
				Path: ".github/workflows/ci.yml", Config: cfg, Project: workspace.Identity(root),
				Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/local
`),
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.severity == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("disabled composite findings remain: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || result.Diagnostics[0].Severity != tc.severity || filepath.ToSlash(result.Diagnostics[0].Path) != "local/action.yml" {
				t.Fatalf("composite override not applied: %+v", result.Diagnostics)
			}
		})
	}
}
