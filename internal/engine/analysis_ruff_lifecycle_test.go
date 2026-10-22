package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"actionlint.kjanat.dev/internal/ruff"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestRuffAnalyzerCompletesBeforeReturning(t *testing.T) {
	var workers sync.WaitGroup
	failure := errors.New("checker failed")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]ruff.Diagnostic{{Code: "F821", Message: "undefined", Filename: filepath.Join(cwd, "actionlint.py"), Location: ruff.Position{Row: 1, Column: 1}, EndLocation: ruff.Position{Row: 1, Column: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	checker := ruff.New(func(_ []string, _ string, callback func([]byte, error) error) {
		workers.Go(func() {
			if err := callback(payload, nil); err != nil {
				t.Error(err)
			}
		})
	}, func() error { workers.Wait(); return failure }, ruffExpressionEnd)
	analyzer := &ruffAnalyzer{checker: checker}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	run := &workflowlang.ExecRun{Run: &workflowlang.String{Value: "x", Pos: pos}, RunPos: pos}
	python := "python"
	scripts := []scriptUnit{{run: run, shell: &python, path: "first.yml"}, {run: run, shell: &python, path: "second.yml"}}
	findings, err := analyzer.AnalyzeScripts(t.Context(), scriptBatch{scripts: scripts})
	if !errors.Is(err, failure) || len(findings) != 2 {
		t.Fatalf("findings=%v error=%v", findings, err)
	}
	if findings[0].Filepath != "first.yml" || findings[1].Filepath != "second.yml" {
		t.Fatal(findings)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	findings, err = analyzer.AnalyzeScripts(ctx, scriptBatch{scripts: scripts})
	if !errors.Is(err, context.Canceled) || len(findings) != 0 {
		t.Fatalf("cancelled: %v %v", findings, err)
	}
}

func TestPreparedScriptsRetainIndependentShells(t *testing.T) {
	source := `on: push
defaults: {run: {shell: python}}
jobs:
  first:
    runs-on: ubuntu-latest
    defaults: {run: {shell: bash}}
    steps:
      - run: echo first
      - run: print('explicit')
        shell: python3
  second:
    runs-on: ubuntu-latest
    steps:
      - run: print('inherited')
`
	workflow, errors := Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	collector := &compositeAnalyzer{collectScripts: true}
	collector.beginWorkflow(workflow)
	var shells []string
	for _, id := range []string{"first", "second"} {
		job := workflow.Jobs[id]
		for _, step := range job.Steps {
			collector.collectScript(step, nil, "", nil, scriptDefaultShell(job.Defaults), collector.workflowScriptShell)
		}
	}
	// Composite scripts have their own declared shell and never inherit callers.
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	collector.collectScript(&workflowlang.Step{Exec: &workflowlang.ExecRun{Run: &workflowlang.String{Value: "echo composite", Pos: pos}, RunPos: pos}}, nil, "action.yml", nil, nil, nil)
	for _, script := range collector.scripts {
		shell := ""
		if script.shell != nil {
			shell = *script.shell
		}
		shells = append(shells, shell)
	}
	if !slices.Equal(shells, []string{"bash", "python3", "python", ""}) {
		t.Fatal(shells)
	}
}
