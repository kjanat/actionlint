package application_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
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
	cfg, err := repoconfig.Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	check := func(result *application.Result, err error) {
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
	check(application.Analyze(t.Context(), application.Options{WorkingDir: "repo"}, []workspace.Source{{Path: ".github/workflows/ci.yml", Content: content, Config: cfg}}))
	check(application.Analyze(t.Context(), application.Options{WorkingDir: "repo"}, []workspace.Source{{Path: ".github/workflows/ci.yml", Content: content, Config: cfg, Project: workspace.Identity("repo")}}))
	options := application.Options{WorkingDir: "repo", ConfigFile: "repo/actionlint.yml"}
	check(application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}}))
	check(application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{filepath.Join("repo", ".github", "workflows", "ci.yml")}}))
	planning := application.Options{WorkingDir: "repo", ConfigFile: "repo/actionlint.yml", Tools: (adapters.Options{ShellCheck: "shellcheck"}).Registrations()}
	for _, path := range []string{workflow, filepath.Join(".github", "workflows", "ci.yml")} {
		needed, err := application.RequiredTools(t.Context(), planning, []string{path})
		if err != nil || !needed["shellcheck"] {
			t.Fatalf("tool planning for %q disagrees with selected file: %+v, %v", path, needed, err)
		}
	}
}

func TestExternalOverridePlanMatchesExecution(t *testing.T) {
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
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, ".github/actionlint.yaml", tc.config)
			workflow := writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)

			options := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellcheckOptions: &processconfig.Options{Executable: &executable, Arguments: []string{"-test.run=^TestShellcheckPlanningHelper$", "--", "shellcheck"}}}).Registrations()}
			needed, err := application.RequiredTools(t.Context(), options, []string{workflow})
			if err != nil || needed["shellcheck"] != tc.enabled {
				t.Fatalf("tool requirement: %+v, %v", needed, err)
			}
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}})
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
