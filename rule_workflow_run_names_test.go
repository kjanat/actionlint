package actionlint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWorkflowRunNames(t *testing.T) {
	root := t.TempDir()
	producer := writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+commandGoodWorkflow)
	writeShellcheckFixture(t, root, ".github/workflows/unnamed.yaml", commandGoodWorkflow)
	writeShellcheckFixture(t, root, ".github/workflows/nested/ignored.yml", "name: Nested\n"+commandGoodWorkflow)
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
			src := "on:\n  workflow_run:\n    workflows: ['" + tc.name + "']\n    types: [completed]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			config, err := ParseConfig([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(src), Project: &Project{root: root}, Config: config}}})
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

func TestWorkflowRunNamesRelativeProject(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+commandGoodWorkflow)
	project, err := NewProject(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Build", "Missing"} {
		t.Run(name, func(t *testing.T) {
			src := "on: {workflow_run: {workflows: [" + name + "], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: ".github/workflows/consumer.yml", Content: []byte(src), Project: project}}})
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
		{"Build CI", "'Build*', '!Build CI'", 0},
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
			project := &Project{root: root}
			consumer := "on: {workflow_run: {workflows: [" + tc.filters + "], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{
				{Path: ".github/workflows/build.yml", Content: []byte("name: '" + tc.name + "'\n" + commandGoodWorkflow), Project: project},
				{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project},
			}})
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
				writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Old\n"+commandGoodWorkflow)
			}
			cfg, err := ParseConfig([]byte("files: {excludes: ['**/build.yml']}"))
			if err != nil {
				t.Fatal(err)
			}
			project := &Project{root: root}
			// The excluded source also contains a lint finding that must remain excluded.
			producerContent := "name: New\nenv: {VALUE: '${{ nonexistent }}'}\n" + commandGoodWorkflow
			consumer := "on: {workflow_run: {workflows: [New], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: ".", Sources: []SourceUnit{
				{Path: ".github/workflows/build.yml", Content: []byte(producerContent), Project: project, Config: cfg},
				{Path: ".github/workflows/consumer.yml", Content: []byte(consumer), Project: project, Config: cfg},
			}})
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
			producer := writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+commandGoodWorkflow)
			consumer := "on: {workflow_run: {workflows: [Missing], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			read := false
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, ReadFile: func(path string) ([]byte, error) {
				if path == producer {
					read = true
					return nil, readErr
				}
				return os.ReadFile(path)
			}, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: &Project{root: root}}}})
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
	producer := writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Old\n"+commandGoodWorkflow)
	consumer := "on: {workflow_run: {workflows: [New], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	project := &Project{root: root}
	sources := []SourceUnit{{Path: producer, Content: []byte("name: New\n" + commandGoodWorkflow), Project: project}, {Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: project}}
	result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: sources})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	// An unreadable name in another workflow prevents claiming a name is missing.
	writeShellcheckFixture(t, root, ".github/workflows/broken.yml", "name: [\n")
	result, err = Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: sources[1:]})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	// Without project context (including playground use), no repository scan.
	sources[1].Project = nil
	result, err = Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: sources[1:]})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}
