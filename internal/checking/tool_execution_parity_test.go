package checking_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
)

func TestRelativeWorkingDirectorySelectionAndOverrides(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	root := filepath.Join(parent, "repo")
	config := `files: {includes: ['.github/workflows/**']}
overrides: [{includes: ['.github/workflows/**'], lint: {rules: {correctness: {if-cond: warn}}}}]
`
	writeFixture(t, root, "actionlint.yml", config)
	workflow := writeFixture(t, root, ".github/workflows/ci.yml", `on: push
jobs: {test: {runs-on: ubuntu-latest, if: false, steps: [{run: echo ok}]}}
`)
	cfg, err := checking.ConfigurationRules().Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	check := func(result *checking.Result, err error) {
		t.Helper()
		if err != nil || result == nil || result.FileCount != 1 {
			t.Fatalf("relative working directory dropped workflow: %+v, %v", result, err)
		}
		if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "if-cond" && d.Severity == "warning" }) {
			t.Fatalf("relative working directory missed override: %+v", result.Diagnostics)
		}
		if !slices.Contains(result.Inputs, workflow) {
			t.Fatalf("workflow input did not resolve to working directory: %+v", result.Inputs)
		}
		for _, diagnostic := range result.Diagnostics {
			if filepath.ToSlash(diagnostic.Path) != ".github/workflows/ci.yml" {
				t.Fatalf("diagnostic path is not repository-relative: %+v", diagnostic)
			}
		}
	}
	content, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatal(err)
	}
	check(checking.Analyze(t.Context(), checking.Options{WorkingDir: "repo"}, []workspace.Source{{Path: ".github/workflows/ci.yml", Content: content, Config: cfg}}))
	check(checking.Analyze(t.Context(), checking.Options{WorkingDir: "repo"}, []workspace.Source{{Path: ".github/workflows/ci.yml", Content: content, Config: cfg, Project: workspace.Identity("repo")}}))
	options := checking.Options{WorkingDir: "repo", ConfigFile: "actionlint.yml"}
	check(checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{workflow}}))
	check(checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{filepath.Join("repo", ".github", "workflows", "ci.yml")}}))
	planning := checking.Options{WorkingDir: "repo", ConfigFile: "actionlint.yml", Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	for _, path := range []string{workflow, filepath.Join(".github", "workflows", "ci.yml")} {
		needed, err := checking.RequiredTools(t.Context(), planning, []string{path})
		if err != nil || !needed["shellcheck"] {
			t.Fatalf("tool planning for %q disagrees with selected file: %+v, %v", path, needed, err)
		}
	}
}

func TestExternalOverridePlanMatchesExecution(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config string
		enabled      bool
	}{
		{"rule disabled by matching override", "overrides: [{includes: ['**'], lint: {rules: {external: {shellcheck: off}}}}]", false},
		{"rule disabled with unrelated override", "lint: {rules: {external: {shellcheck: off}}}\noverrides: [{includes: [other.yml], lint: {rules: {suspicious: on}}}]", false},
		{"tool disabled with unrelated override", "tools: {shellcheck: false}\noverrides: [{includes: [other.yml], lint: {rules: {suspicious: on}}}]", false},
		{"tool disabled by matching override", "overrides: [{includes: ['**'], tools: {shellcheck: false}}]", false},
		{"enabled with unrelated override", "overrides: [{includes: [other.yml], lint: {rules: {suspicious: on}}}]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, ".github/actionlint.yaml", tc.config)
			workflow := writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)

			options := checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Process: &processconfig.Options{Executable: &executable, Arguments: []string{"-test.run=^TestShellcheckPlanningHelper$", "--", "shellcheck"}}}})}
			needed, err := checking.RequiredTools(t.Context(), options, []string{workflow})
			if err != nil || needed["shellcheck"] != tc.enabled {
				t.Fatalf("tool requirement: %+v, %v", needed, err)
			}
			result, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatalf("ShellCheck execution failed, plan=%+v, error=%v", needed, err)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "shellcheck" })
			if found != tc.enabled {
				t.Fatalf("ShellCheck execution disagrees with plan: %+v", result.Diagnostics)
			}
		})
	}
}
