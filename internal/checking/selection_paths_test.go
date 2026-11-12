package checking_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"

	"github.com/google/go-cmp/cmp"
)

func TestCallerRelativeDisplaySpelling(t *testing.T) {
	for _, spelling := range []string{"ci.yml", "./ci.yml", "absolute"} {
		t.Run(spelling, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			workflow := writeFixture(t, root, "ci.yml", badWorkflow)
			path := spelling
			if spelling == "absolute" {
				path = workflow
			}
			readPath := ""
			session := checking.Options{WorkingDir: root, ReadFile: func(name string) ([]byte, error) {
				if absolute, err := filepath.Abs(name); err == nil && absolute == workflow {
					readPath = name
				}
				return os.ReadFile(name)
			}}
			var err error

			result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
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
	session := checking.Options{WorkingDir: parent}

	result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{relative}})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) == 0 || !slices.Contains(result.Inputs, workflow) {
		t.Fatalf("caller-relative workflow was dropped: %+v, %v", result, err)
	}
	project, err := workspace.NewProjects(checking.ConfigurationRules()).At(workflow)
	if err != nil {
		t.Fatal(err)
	}
	result, err = checking.Analyze(t.Context(), checking.Options{WorkingDir: parent}, []workspace.Source{{Path: relative, Content: []byte(badWorkflow), Project: project, Config: project.Config()}})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) == 0 || !slices.Contains(result.Inputs, workflow) {
		t.Fatalf("direct caller-relative workflow was dropped: %+v, %v", result, err)
	}
}

func TestCallerRelativeOverlaySelection(t *testing.T) {
	for _, spelling := range []string{"relative", "absolute"} {
		t.Run(spelling, func(t *testing.T) {
			parent := t.TempDir()
			t.Chdir(parent)
			root := filepath.Join(parent, "repo")
			writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)
			working := root
			if spelling == "relative" {
				working = "repo"
			}
			options := checking.Options{WorkingDir: working, SkipProjectConfig: true, SourceOverlays: []checking.SourceOverlay{{Path: ".github/workflows/ci.yml", Content: []byte(badWorkflow)}}}
			result, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{filepath.Join("repo", ".github", "workflows", "ci.yml")}})
			if err != nil || result == nil || result.FileCount != 1 || len(result.Diagnostics) == 0 {
				t.Fatalf("caller input missed WorkingDir-relative overlay: %+v, %v", result, err)
			}
			for _, diagnostic := range result.Diagnostics {
				if filepath.ToSlash(diagnostic.Path) != ".github/workflows/ci.yml" {
					t.Fatalf("overlay diagnostic lost display path: %+v", diagnostic)
				}
			}
		})
	}
}

func TestExcludedSelectionRetainsConfigurations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	base := writeFixture(t, root, "base.yml", "files: {includes: []}\n")
	config := writeFixture(t, root, ".github/actionlint.yaml", "extends: ['../base.yml']\n")
	workflow := writeFixture(t, root, ".github/workflows/ci.yml", goodWorkflow)
	session := checking.Options{WorkingDir: root}
	var err error

	for range 2 {
		result, err := checking.Check(t.Context(), session, inputmodel.FromFiles(workspace.FileSelection{Files: []workspace.File{{Path: workflow}}}))
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
	loaded, err := checking.ConfigurationRules().Read(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: workflow, Content: []byte(goodWorkflow), Config: loaded.Config, ConfigProvenance: loaded.Provenance}})
	if err != nil || result.FileCount != 0 || !slices.Contains(result.Inputs, base) || !slices.Contains(result.Inputs, config) {
		t.Fatalf("direct analysis lost excluded configuration inputs: %+v, %v", result, err)
	}
}

func TestPhysicalProjectAliasSelection(t *testing.T) {
	t.Parallel()
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
		cfg, err := checking.ConfigurationRules().Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, ".github/actionlint.yaml", config)
		for _, roots := range [][2]string{{root, alias}, {alias, root}} {
			project, err := workspace.NewProject(checking.ConfigurationRules(), roots[0])
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(roots[1], ".github", "workflows", "ci.yml")
			check := func(result *checking.Result, err error) {
				t.Helper()
				if err != nil || result == nil || result.FileCount != 1 || !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "if-cond" && d.Severity == "warning" }) {
					t.Fatalf("filtered=%t: alias selection/override lost: %+v, %v", filtered, result, err)
				}
			}
			check(checking.Analyze(t.Context(), checking.Options{WorkingDir: parent}, []workspace.Source{{Path: path, Content: []byte(content), Project: project, Config: cfg}}))
			var reads []string
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: parent, ReadFile: func(name string) ([]byte, error) {
				reads = append(reads, name)
				return os.ReadFile(name)
			}, Rules: checking.ConfigurationRules()})
			if err != nil {
				t.Fatal(err)
			}
			selection, err := session.Files([]string{path}, project)
			if err != nil {
				t.Fatal(err)
			}
			check(checking.Analyze(t.Context(), checking.Options{WorkingDir: parent}, selection.Sources))
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
		session := checking.Options{WorkingDir: "repo"}
		var err error

		result, err := checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(badWorkflow), NormalizePath: true, Name: name})
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
