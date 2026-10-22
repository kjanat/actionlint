package application_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"

	"github.com/google/go-cmp/cmp"
)

func TestCallerRelativeDisplaySpelling(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	workflow := writeFixture(t, root, "ci.yml", badWorkflow)
	for _, path := range []string{"ci.yml", "./ci.yml", workflow} {
		t.Run(path, func(t *testing.T) {
			readPath := ""
			session := application.Options{WorkingDir: root, ReadFile: func(name string) ([]byte, error) {
				if absolute, err := filepath.Abs(name); err == nil && absolute == workflow {
					readPath = name
				}
				return os.ReadFile(name)
			}}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{path}})
			if err != nil || result == nil || len(result.Diagnostics) == 0 {
				t.Fatalf("missing diagnostics: %+v, %v", result, err)
			}
			if readPath != path {
				t.Fatalf("reader received %q, want caller spelling %q", readPath, path)
			}
			want := path
			if filepath.IsAbs(path) {
				want = "ci.yml"
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Path != want {
					t.Fatalf("diagnostic path %q, want %q", diagnostic.Path, want)
				}
			}
			if !slices.Contains(result.Inputs, workflow) {
				t.Fatalf("absolute workflow input missing: %v", result.Inputs)
			}
		})
	}
}

func TestCallerRelativeSelection(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	root := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, ".github/actionlint.yaml", "files: {includes: ['.github/workflows/**']}\n")
	workflow := writeFixture(t, root, ".github/workflows/ci.yml", badWorkflow)
	relative, err := filepath.Rel(parent, workflow)
	if err != nil {
		t.Fatal(err)
	}
	session := application.Options{WorkingDir: parent}

	result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{relative}})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) == 0 || !slices.Contains(result.Inputs, workflow) {
		t.Fatalf("caller-relative workflow was dropped: %+v, %v", result, err)
	}
	project, err := workspace.NewProjects().At(workflow)
	if err != nil {
		t.Fatal(err)
	}
	result, err = application.Analyze(t.Context(), application.Options{WorkingDir: parent}, []workspace.Source{{Path: relative, Content: []byte(badWorkflow), Project: project, Config: project.Config()}})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) == 0 || !slices.Contains(result.Inputs, workflow) {
		t.Fatalf("direct caller-relative workflow was dropped: %+v, %v", result, err)
	}
}

func TestExcludedSelectionRetainsConfigurations(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	base := writeFixture(t, root, "base.yml", "files: {includes: []}\n")
	config := writeFixture(t, root, ".github/actionlint.yaml", "extends: ['../base.yml']\n")
	workflow := writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)
	session := application.Options{WorkingDir: root}
	var err error

	for range 2 {
		result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{base, config}
		slices.Sort(want)
		if diff := cmp.Diff(want, result.Inputs); diff != "" {
			t.Fatalf("excluded selection dependencies (-want +got):\n%s", diff)
		}
		if result.FileCount != 0 || len(result.Configurations) != 1 || result.Configurations[0].File != config {
			t.Fatalf("excluded selection lost provenance: %+v", result)
		}
	}
	// Direct API callers also consume config dependencies before filtering.
	cfg, err := repoconfig.Load(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: workflow, Content: []byte(goodWorkflow), Config: cfg}})
	if err != nil || result.FileCount != 0 || !slices.Contains(result.Inputs, base) || !slices.Contains(result.Inputs, config) {
		t.Fatalf("direct analysis lost excluded configuration inputs: %+v, %v", result, err)
	}
}

func TestPhysicalProjectAliasSelection(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "physical")
	content := `on: push
jobs: {test: {runs-on: ubuntu-latest, if: false, steps: [{run: echo ok}]}}
`
	writeFixture(t, root, ".github/workflows/ci.yml", content)
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, filtered := range []bool{false, true} {
		config := "overrides: [{includes: ['.github/workflows/**'], lint: {rules: {correctness: {if-cond: warn}}}}]\n"
		if filtered {
			config += "files: {includes: ['.github/workflows/**']}\n"
		}
		cfg, err := repoconfig.Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, ".github/actionlint.yaml", config)
		for _, roots := range [][2]string{{root, alias}, {alias, root}} {
			project, err := workspace.NewProject(roots[0])
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(roots[1], ".github", "workflows", "ci.yml")
			check := func(result *application.Result, err error) {
				t.Helper()
				if err != nil || result == nil || result.FileCount != 1 || !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "if-cond" && d.Severity == "warning" }) {
					t.Fatalf("filtered=%t: alias selection/override lost: %+v, %v", filtered, result, err)
				}
			}
			check(application.Analyze(t.Context(), application.Options{WorkingDir: parent}, []workspace.Source{{Path: path, Content: []byte(content), Project: project, Config: cfg}}))
			var reads []string
			session, err := workspace.New(workspace.Options{WorkingDir: parent, ReadFile: func(name string) ([]byte, error) {
				reads = append(reads, name)
				return os.ReadFile(name)
			}})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := session.Files([]string{path}, project)
			if err != nil {
				t.Fatal(err)
			}
			check(application.Analyze(t.Context(), application.Options{WorkingDir: parent}, selection.Sources))
			if !slices.Contains(reads, path) {
				t.Fatalf("reader lost caller path %q: %v", path, reads)
			}
		}
	}
}

func TestRelativeWorkingDirectoryStdinPaths(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	if err := os.Mkdir("repo", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "ci.yml"} {
		session := application.Options{WorkingDir: "repo"}
		var err error

		result, err := application.Check(t.Context(), session, application.Input{Kind: application.Stdin, Reader: strings.NewReader(badWorkflow), NormalizePath: true, Name: name})
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
