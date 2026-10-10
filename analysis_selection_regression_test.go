package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestRelativeWorkingDirectorySelectionAndOverrides(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	root := filepath.Join(parent, "repo")
	config := "files: {includes: ['.github/workflows/**']}\noverrides: [{includes: ['.github/workflows/**'], lint: {rules: {correctness: {if-cond: warn}}}}]\n"
	writeShellcheckFixture(t, root, "actionlint.yml", config)
	workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs: {test: {runs-on: ubuntu-latest, if: false, steps: [{run: echo ok}]}}\n")
	cfg, err := ParseConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	check := func(result *AnalysisResult, err error) {
		t.Helper()
		if err != nil || result == nil || result.FileCount() != 1 {
			t.Fatalf("relative working directory dropped workflow: %+v, %v", result, err)
		}
		if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "if-cond" && d.Severity == "warning" }) {
			t.Fatalf("relative working directory missed override: %+v", result.Diagnostics)
		}
		if !slices.Contains(result.Inputs, workflow) {
			t.Fatalf("workflow input did not resolve to working directory: %+v", result.Inputs)
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Path != filepath.Join(".github", "workflows", "ci.yml") {
				t.Fatalf("diagnostic path is not repository-relative: %+v", diagnostic)
			}
		}
	}
	content, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatal(err)
	}
	check(Analyze(t.Context(), AnalysisRequest{WorkingDir: "repo", Sources: []SourceUnit{{Path: ".github/workflows/ci.yml", Content: content, Config: cfg}}}))
	check(Analyze(t.Context(), AnalysisRequest{WorkingDir: "repo", Sources: []SourceUnit{{Path: ".github/workflows/ci.yml", Content: content, Config: cfg, Project: &Project{root: "repo"}}}}))
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: "repo", ConfigFile: "repo/actionlint.yml"})
	if err != nil {
		t.Fatal(err)
	}
	check(session.Files([]string{workflow}, nil))
	check(session.Files([]string{filepath.Join("repo", ".github", "workflows", "ci.yml")}, nil))
	planning, err := NewAnalysisSession(AnalysisOptions{WorkingDir: "repo", ConfigFile: "repo/actionlint.yml", Shellcheck: "shellcheck"})
	if err != nil {
		t.Fatal(err)
	}
	needed, err := planning.RequiredTools([]string{filepath.Join("repo", ".github", "workflows", "ci.yml")})
	if err != nil || !needed.Shellcheck {
		t.Fatalf("tool planning disagrees with selected file: %+v, %v", needed, err)
	}
}

func TestRelativeWorkingDirectoryStdinPaths(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	if err := os.Mkdir("repo", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "ci.yml"} {
		session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: "repo", StdinFileName: name})
		if err != nil {
			t.Fatal(err)
		}
		result, err := session.ReadStdin(strings.NewReader(commandBadWorkflow), true)
		if err != nil || len(result.Diagnostics) == 0 {
			t.Fatalf("stdin analysis: %+v, %v", result, err)
		}
		want := name
		if want == "" {
			want = "<stdin>"
		}
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Path != want {
				t.Fatalf("stdin path changed: %+v", diagnostic)
			}
		}
		if name != "" && !slices.Contains(result.Inputs, filepath.Join(parent, "repo", name)) {
			t.Fatalf("virtual input does not match working directory: %v", result.Inputs)
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
