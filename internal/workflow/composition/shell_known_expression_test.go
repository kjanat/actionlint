package composition

import (
	"slices"
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
	snapshot := workflowSnapshot(t, content, document)
	workflow := workflowView(t, snapshot)
	collector := &compositeAnalyzer{compositionSession: &compositionSession{collectScripts: true, resolveScriptExecution: true}, actions: actionlocal.NewResolver(nil, nil)}
	startWorkflowEntry(t, collector, snapshot)
	if err := collector.beginWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"python", "bash"} {
		job := workflowJob(t, workflow, id)
		if err := collector.beginJob(job); err != nil {
			t.Fatal(err)
		}
		if err := collector.collectScript(slices.Collect(job.Steps())[0], nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		if got := collector.scripts[len(collector.scripts)-1].Execution.Shell.Name; got != id {
			t.Fatalf("job %s resolved shell %q", id, got)
		}
	}
	job := workflowJob(t, workflow, "python")
	if job.Defaults().Run().Shell().Present() || len(slices.Collect(job.RunsOn().Labels())) != 0 {
		t.Fatal("shell resolution mutated the published declarations")
	}
	original := document.Workflow.Jobs["python"]
	if original.Defaults.Run.Shell != nil || len(original.RunsOn.Labels) != 0 {
		t.Fatal("shell resolution mutated the decoder model")
	}
}
