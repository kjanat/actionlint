package actionlint

import (
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
