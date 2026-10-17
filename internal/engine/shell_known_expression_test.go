package engine

import (
	"testing"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
)

func TestKnownShellStateReset(t *testing.T) {
	workflow, errors := Parse([]byte(`on: push
jobs:
  python:
    runs-on: ${{ fromJSON('{"labels":"windows-latest"}') }}
    defaults:
      run: ${{ fromJSON('{"shell":"python"}') }}
    steps:
      - run: print("hello")
  bash:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`))
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	collector := &compositeAnalyzer{collectScripts: true, resolveScriptExecution: true, actions: actionlocal.NewResolver(nil, nil)}
	collector.beginWorkflow(workflow)
	for _, id := range []string{"python", "bash"} {
		job := workflow.Jobs[id]
		collector.beginJob(job)
		collector.collectScript(job.Steps[0], nil, "", nil, nil, nil)
		if got := collector.scripts[len(collector.scripts)-1].execution.shell.Name; got != id {
			t.Fatalf("job %s resolved shell %q", id, got)
		}
	}
	job := workflow.Jobs["python"]
	if job.Defaults.Run.Shell != nil || len(job.RunsOn.Labels) != 0 {
		t.Fatal("shell resolution mutated the source AST")
	}
}
