package composites

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/parsing"

	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
)

func TestKnownShellStateReset(t *testing.T) {
	t.Parallel()
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
	document := parsing.Workflow(content)
	if len(document.SyntaxErrors) != 0 {
		t.Fatal(document.SyntaxErrors)
	}
	registry, snapshot := registeredWorkflowSnapshot(t, content, document)
	workflow := workflowView(t, snapshot)
	collector := &compositeAnalyzer{compositionSession: &compositionSession{collectScripts: true, resolveScriptExecution: true}, actions: workspaceactions.NewResolver(nil, nil)}
	startWorkflowEntry(t, collector, registry, snapshot)
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
