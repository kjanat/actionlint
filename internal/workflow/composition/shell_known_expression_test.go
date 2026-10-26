package composition

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/documentparse"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
)

func TestKnownShellStateReset(t *testing.T) {
	content := []byte(`on: push
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
`)
	document := documentparse.Workflow("fixture.yml", content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	workflow := document.Workflow
	collector := &compositeAnalyzer{collectScripts: true, resolveScriptExecution: true, actions: actionlocal.NewResolver(nil, nil)}
	startWorkflowEntry(t, collector, workflowSnapshot(t, content, document))
	if err := collector.beginWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"python", "bash"} {
		job := workflow.Jobs[id]
		if err := collector.beginJob(job); err != nil {
			t.Fatal(err)
		}
		if err := collector.collectScript(job.Steps[0], nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		if got := collector.scripts[len(collector.scripts)-1].Execution.Shell.Name; got != id {
			t.Fatalf("job %s resolved shell %q", id, got)
		}
	}
	job := workflow.Jobs["python"]
	if job.Defaults.Run.Shell != nil || len(job.RunsOn.Labels) != 0 {
		t.Fatal("shell resolution mutated the source AST")
	}
}
