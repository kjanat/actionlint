package engine

import (
	"slices"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

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
