package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestExcludedSelectionRetainsConfigurations(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	base := writeShellcheckFixture(t, root, "base.yml", "files: {includes: []}\n")
	config := writeShellcheckFixture(t, root, ".github/actionlint.yaml", "extends: ['../base.yml']\n")
	workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := session.Files([]string{workflow}, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{base, config}
		slices.Sort(want)
		if diff := cmp.Diff(want, result.Inputs); diff != "" {
			t.Fatalf("excluded selection dependencies (-want +got):\n%s", diff)
		}
		if result.FileCount() != 0 || len(result.Configurations) != 1 || result.Configurations[0].File != config {
			t.Fatalf("excluded selection lost provenance: %+v", result)
		}
	}
	// Direct API callers also consume config dependencies before filtering.
	cfg, err := ReadConfigFile(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: workflow, Content: []byte(commandGoodWorkflow), Config: cfg}}})
	if err != nil || result.FileCount() != 0 || !slices.Contains(result.Inputs, base) || !slices.Contains(result.Inputs, config) {
		t.Fatalf("direct analysis lost excluded configuration inputs: %+v, %v", result, err)
	}
}

func TestCallerRelativeSelection(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	root := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "files: {includes: ['.github/workflows/**']}\n")
	workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandBadWorkflow)
	relative, err := filepath.Rel(parent, workflow)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: parent})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{relative}, nil)
	if err != nil || result.FileCount() != 1 || len(result.Diagnostics) == 0 || !slices.Contains(result.Inputs, workflow) {
		t.Fatalf("caller-relative workflow was dropped: %+v, %v", result, err)
	}
	project, err := session.projects.At(workflow)
	if err != nil {
		t.Fatal(err)
	}
	result, err = Analyze(t.Context(), AnalysisRequest{WorkingDir: parent, Sources: []SourceUnit{{Path: relative, Content: []byte(commandBadWorkflow), Project: project, Config: project.Config()}}})
	if err != nil || result.FileCount() != 1 || len(result.Diagnostics) == 0 || !slices.Contains(result.Inputs, workflow) {
		t.Fatalf("direct caller-relative workflow was dropped: %+v, %v", result, err)
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
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", tc.config)
			workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
			created := false
			session, err := NewAnalysisSession(AnalysisOptions{
				WorkingDir:        root,
				ShellcheckOptions: &ExternalCommandOptions{Executable: &executable, Arguments: []string{"-test.run=^TestKnownShellCommandHelper$", "--", "shellcheck"}},
				OnRulesCreated: func(rules []Rule) []Rule {
					for _, rule := range rules {
						if _, ok := rule.(*RuleShellcheck); ok {
							created = true
						}
					}
					return rules
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			needed, err := session.RequiredTools([]string{workflow})
			if err != nil || needed.Shellcheck != tc.enabled {
				t.Fatalf("tool requirement: %+v, %v", needed, err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil || created != tc.enabled {
				t.Fatalf("ShellCheck registration=%v, plan=%+v, error=%v", created, needed, err)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" })
			if found != tc.enabled {
				t.Fatalf("ShellCheck execution disagrees with plan: %+v", result.Diagnostics)
			}
		})
	}
}
