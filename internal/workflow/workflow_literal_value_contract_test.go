package workflow_test

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestWorkflowLiteralStaticValues(t *testing.T) {
	source := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: ${{ 'build' }}
        run: echo ok
        shell: ${{ 'bash' }}
      - uses: ${{ 'docker://alpine:3' }}
        with: {entrypoint: sh, args: -c}
`)
	w, errs := workflowlang.ParseDocument(source)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	steps := w.Jobs["test"].Steps
	if steps[0].ID.Value != "build" || steps[0].ID.Pos.Line != 6 || steps[0].Exec.(*workflowlang.ExecRun).Shell.Value != "bash" {
		t.Fatal("static values or positions changed")
	}
	action := steps[1].Exec.(*workflowlang.ExecAction)
	if action.Uses.Value != "docker://alpine:3" || action.Entrypoint == nil || action.Args == nil || len(action.Inputs) != 0 {
		t.Fatalf("Docker input dispatch lost: %#v", action)
	}
}
