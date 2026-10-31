package application

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/source"
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
				result, err := capturedDependencyOverrideFixture(t, root, dependency, cfg)
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
				result, err := capturedDependencyOverrideFixture(t, root, dependency, cfg)
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

func capturedDependencyOverrideFixture(t *testing.T, root, dependency string, cfg *repoconfig.Config) (*analysisResult, error) {
	t.Helper()
	cache := newCapturedWorkflowCache(t, root, root, nil)
	path := filepath.Join(root, dependency)
	coordinates := source.YAML
	if filepath.Ext(path) == ".sh" {
		coordinates = source.RawLF
	}
	// The synthetic producer observes bytes captured before traversal by the
	// same owner used for workflow analysis and final publication.
	dependencySource, err := cache.snapshots.Capture(path, []byte("dependency content"), coordinates)
	if err != nil {
		return nil, err
	}
	reference := sourceid.New(dependencySource.Revision().Document(), dependencySource.Bytes()).Ref("", "document", "", "$")
	workflow := sourceUnit{Path: ".github/workflows/ci.yml", Content: []byte(commandGoodWorkflow), Config: cfg, Project: workspace.Identity(root)}
	findings, err := checkWithCaches(t, t.Context(), analysisRequest{
		WorkingDir: root,
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			rule := workflowvisit.NewBase("expression-reference", "dependency finding")
			finding := findingmodel.At(&workflowlang.Pos{Line: 1, Col: 1}, rule.Name(), "dependency finding")
			finding.Path, finding.Source = path, dependencySource.Bytes()
			finding.Reference = &reference
			finding.RawScriptSource = coordinates == source.RawLF
			rule.Report(finding)
			return []workflowvisit.Rule{&rule}
		},
	}, workflow, nil, nil, cache)
	if err != nil {
		return nil, err
	}
	entry, state, err := cache.captures.Lookup(cache.snapshots.Path(workflow.Path))
	if err != nil {
		return nil, err
	}
	if state != capture.Captured {
		t.Fatalf("workflow fixture was not captured: %s", state)
	}
	if err := cache.captures.Close(t.Context()); err != nil {
		return nil, err
	}
	rows, err := cache.captures.Sources()
	if err != nil {
		return nil, err
	}
	result := &analysisResult{files: []analyzedFile{{captured: entry, source: workflow, errors: findings}}}
	if err := result.revalidateCaptures(cache.captures, rows, root, nil); err != nil {
		return result, err
	}
	result.collectDiagnostics()
	for _, diagnostic := range result.Diagnostics {
		if !strings.HasSuffix(filepath.ToSlash(diagnostic.Path), dependency) {
			t.Fatalf("synthetic finding reported at %q, not dependency %q", diagnostic.Path, dependency)
		}
	}
	return result, nil
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
