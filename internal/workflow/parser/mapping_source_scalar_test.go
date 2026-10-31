package parser

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/mapping"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func TestActionInputMappingPreservesSourceScalarTags(t *testing.T) {
	workflow, errs := Parse([]byte("on: push\nenv:\n  NULL: key-value\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n        with:\n          NULL: key-value\n          null-value: null\n          null-text: 'null'\n          false-value: false\n          false-text: 'false'\n"))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	action, ok := workflow.Jobs["test"].Steps[0].Exec.(*workflowlang.ExecAction)
	if !ok {
		t.Fatal("fixture action missing")
	}
	inputs := mapping.Resolve(workflowlang.InputMapping(action))
	if got := inputs.Lookup("null"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("plain null-looking key state=%v kind=%v", got.State(), got.Kind())
	}
	if got, ok := inputs.Lookup("null").Text(); !ok || got != "key-value" {
		t.Fatalf("plain null-looking key value=%q known=%v", got, ok)
	}
	env := mapping.Resolve(workflowlang.EnvMappingView(workflow.Env)).Lookup("null")
	if text, ok := env.Text(); !ok || text != "key-value" {
		t.Fatalf("plain null-looking environment key value=%q known=%v", text, ok)
	}
	if got := inputs.Lookup("null-value"); !got.Known() || got.Kind() != value.Null {
		t.Fatalf("unquoted null input state=%v kind=%v", got.State(), got.Kind())
	}
	if got := inputs.Lookup("null-text"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("quoted null input state=%v kind=%v", got.State(), got.Kind())
	}
	if got, known := inputs.Lookup("false-value").Bool(); !known || got {
		t.Fatalf("unquoted false input=%v known=%v", got, known)
	}
	if got := inputs.Lookup("false-text"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("quoted false input state=%v kind=%v", got.State(), got.Kind())
	}
}
