package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestWorkflowRunNames(t *testing.T) {
	root := t.TempDir()
	producer := testutil.WriteFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
	testutil.WriteFixture(t, root, ".github/workflows/unnamed.yaml", testutil.GoodWorkflow)
	testutil.WriteFixture(t, root, ".github/workflows/nested/ignored.yml", "name: Nested\n"+testutil.GoodWorkflow)
	for _, tc := range []struct {
		name, config string
		want         int
	}{
		{"Build", "", 0},
		{"build", "", 1},
		{"Typo", "", 1},
		{"Nested", "", 1},
		{".github/workflows/unnamed.yaml", "", 0},
		{"Build", "files: {excludes: ['**/build.yml']}", 0},
		{"Typo", "lint: {rules: {correctness: {workflow-run-names: off}}}", 0},
	} {
		t.Run(tc.name+tc.config, func(t *testing.T) {
			src := `on:
  workflow_run:
    workflows: ['` + tc.name + `']
    types: [completed]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			config, err := repoconfig.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(src), Project: workspace.Identity(root), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("got %+v", result.Diagnostics)
			}
			if tc.want > 0 && (result.Diagnostics[0].Rule != "workflow-run-names" || result.Diagnostics[0].Start.Line != 3) {
				t.Fatal(result.Diagnostics)
			}
			if !strings.Contains(tc.config, "off") && !slices.Contains(result.Inputs, producer) {
				t.Fatalf("producer not tracked: %v", result.Inputs)
			}
		})
	}
}

func TestWorkflowRunNamesLiteralNames(t *testing.T) {
	for _, tc := range []struct {
		name, reference string
		want            int
	}{
		{"${{ 'Build' }}", "Build", 0},
		{"${{ 'Build' }}", "Typo", 1},
		{"${{ 'Build''s CI' }}", "Build''s CI", 0},
		{"${{ 'Build''s CI' }}", "Typo", 1},
		{"${{ '' }}", ".github/workflows/build.yml", 0},
		{"${{ '' }}", "Typo", 1},
		{"${{ github.ref }}", "Typo", 0},
		{"${{ format('Build') }}", "Typo", 0},
	} {
		for _, inMemory := range []bool{false, true} {
			t.Run(tc.name+tc.reference+strconv.FormatBool(inMemory), func(t *testing.T) {
				root := t.TempDir()
				project := workspace.Identity(root)
				producer := "name: " + tc.name + "\n" + testutil.GoodWorkflow
				consumer := "on: {workflow_run: {workflows: ['" + tc.reference + `'], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				sources := []workspace.Source{{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project}}
				if inMemory {
					cfg, err := repoconfig.Parse([]byte("files: {excludes: ['**/build.yml']}"))
					if err != nil {
						t.Fatal(err)
					}
					sources = append(sources, workspace.Source{Path: ".github/workflows/build.yml", Content: []byte(producer), Project: project, Config: cfg})
				} else {
					testutil.WriteFixture(t, root, ".github/workflows/build.yml", producer)
				}
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, sources)
				if err != nil || len(result.Diagnostics) != tc.want {
					t.Fatalf("got %+v, %v; want %d findings", result, err, tc.want)
				}
				if tc.want > 0 && result.Diagnostics[0].Rule != "workflow-run-names" {
					t.Fatal(result.Diagnostics)
				}
			})
		}
	}
}

func TestWorkflowRunNamesFilesystemPathCasing(t *testing.T) {
	for _, disk := range []string{"name: Old\n" + testutil.GoodWorkflow, "name: [\n"} {
		for _, reference := range []string{"New", "Old", "Typo"} {
			t.Run(disk+reference, func(t *testing.T) {
				root := t.TempDir()
				producer := testutil.WriteFixture(t, root, ".github/workflows/build.yml", disk)
				if _, err := os.Stat(strings.ToUpper(root)); err != nil {
					t.Skip("requires a case-insensitive filesystem")
				}
				project := workspace.Identity(strings.ToUpper(root))
				consumer := "on: {workflow_run: {workflows: [" + reference + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, ReadFile: func(path string) ([]byte, error) {
					if strings.EqualFold(path, producer) {
						t.Errorf("read stale on-disk producer %q", path)
						return nil, os.ErrPermission
					}
					return os.ReadFile(path)
				}}, []workspace.Source{
					{Path: strings.ToLower(producer), Content: []byte("name: New\n" + testutil.GoodWorkflow), Project: project},
					{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: project},
				})
				want := 1
				if reference == "New" {
					want = 0
				}
				if err != nil || len(result.Diagnostics) != want {
					t.Fatalf("got %+v, %v; want %d findings", result, err, want)
				}
				if !slices.Contains(result.Inputs, strings.ToLower(producer)) {
					t.Fatalf("in-memory producer path missing from inputs: %v", result.Inputs)
				}
			})
		}
	}
}

func TestWorkflowRunNamesRelativeProject(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	testutil.WriteFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
	project, err := workspace.NewProject(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Build", "Missing"} {
		t.Run(name, func(t *testing.T) {
			src := "on: {workflow_run: {workflows: [" + name + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: ".github/workflows/consumer.yml", Content: []byte(src), Project: project}})
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if name == "Missing" {
				want = 1
			}
			if len(result.Diagnostics) != want || (want == 1 && result.Diagnostics[0].Rule != "workflow-run-names") {
				t.Fatalf("got %+v, want %d workflow-run-names findings", result.Diagnostics, want)
			}
		})
	}
}

func TestWorkflowRunNamesPatterns(t *testing.T) {
	for _, tc := range []struct {
		name, filters string
		want          int
	}{
		{"Build CI", "'Build*'", 0},
		{"Build CI", "'Missing*'", 1},
		{"Build CI", "'Build*', '!Build CI'", 1},
		{"Build CI", "'Build*', '!Missing*'", 0},
		{"Build CI", "'Build*', '!Build*', 'Build CI'", 0},
		{"Build C++", `'Build C\+\+'`, 0},
		{"[Build]", `'\[Build\]'`, 0},
		{"!Build", `'\!Build'`, 0},
		{"Build 123", "'Build [0-9]+'", 0},
		{"Build/CI", "'Build*'", 1},
		{"Build/CI", "'Build**'", 0},
	} {
		t.Run(tc.name+tc.filters, func(t *testing.T) {
			root := t.TempDir()
			project := workspace.Identity(root)
			consumer := "on: {workflow_run: {workflows: [" + tc.filters + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, []workspace.Source{
				{Path: ".github/workflows/build.yml", Content: []byte("name: '" + tc.name + "'\n" + testutil.GoodWorkflow), Project: project},
				{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project},
			})
			if err != nil || len(result.Diagnostics) != tc.want {
				t.Fatalf("%+v %v; want %d findings", result, err, tc.want)
			}
			if tc.want > 0 && result.Diagnostics[0].Rule != "workflow-run-names" {
				t.Fatal(result.Diagnostics)
			}
		})
	}
}

func TestWorkflowRunNamesExcludedInMemory(t *testing.T) {
	for _, onDisk := range []bool{false, true} {
		name := "memory only"
		if onDisk {
			name = "newer than disk"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			producer := filepath.Join(root, ".github/workflows/build.yml")
			if onDisk {
				testutil.WriteFixture(t, root, ".github/workflows/build.yml", "name: Old\n"+testutil.GoodWorkflow)
			}
			cfg, err := repoconfig.Parse([]byte("files: {excludes: ['**/build.yml']}"))
			if err != nil {
				t.Fatal(err)
			}
			project := workspace.Identity(root)
			// The excluded source also contains a lint finding that must remain excluded.
			producerContent := `name: New
env: {VALUE: '${{ nonexistent }}'}
` + testutil.GoodWorkflow
			consumer := `on: {workflow_run: {workflows: [New], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: "."}, []workspace.Source{
				{Path: ".github/workflows/build.yml", Content: []byte(producerContent), Project: project, Config: cfg},
				{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project, Config: cfg},
			})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("%+v %v", result, err)
			}
			if !slices.Contains(result.Inputs, producer) {
				t.Fatalf("excluded producer not tracked: %v", result.Inputs)
			}
		})
	}
}

func TestWorkflowRunNamesUnreadableSibling(t *testing.T) {
	for _, readErr := range []error{os.ErrPermission, os.ErrNotExist, context.Canceled, context.DeadlineExceeded} {
		t.Run(readErr.Error(), func(t *testing.T) {
			root := t.TempDir()
			producer := testutil.WriteFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
			consumer := `on: {workflow_run: {workflows: [Missing], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
			read := false
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, ReadFile: func(path string) ([]byte, error) {
				if path == producer {
					read = true
					return nil, readErr
				}
				return os.ReadFile(path)
			}}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: workspace.Identity(root)}})
			if !read {
				t.Fatal("sibling was not read")
			}
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				if !errors.Is(err, readErr) {
					t.Fatalf("got %v, want %v", err, readErr)
				}
			} else if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}

func TestWorkflowRunNamesInMemoryAndIncomplete(t *testing.T) {
	root := t.TempDir()
	producer := testutil.WriteFixture(t, root, ".github/workflows/build.yml", "name: Old\n"+testutil.GoodWorkflow)
	consumer := `on: {workflow_run: {workflows: [New], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
	project := workspace.Identity(root)
	sources := []workspace.Source{{Path: producer, Content: []byte("name: New\n" + testutil.GoodWorkflow), Project: project}, {Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: project}}
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root}, sources)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	testutil.
		// An unreadable name in another workflow prevents claiming a name is missing.
		WriteFixture(t, root, ".github/workflows/broken.yml", "name: [\n")
	result, err = application.Analyze(t.Context(), application.Options{WorkingDir: root}, sources[1:])
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	// Without project context (including playground use), no repository scan.
	sources[1].Project = nil
	result, err = application.Analyze(t.Context(), application.Options{WorkingDir: root}, sources[1:])
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}
