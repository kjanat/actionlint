package scripts

import (
	"context"
	"reflect"
	"testing"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestInputRequestsIdentifyLanguageAndSite(t *testing.T) {
	t.Parallel()
	sites := []checkinputs.ScriptInput{{Action: "owner/action/SubPath", Input: "source", Language: "python"}, {Action: "owner/action", Input: "code", Language: "javascript"}, {Action: "owner/action", Input: "other", Language: "python"}}
	var names []string
	registration, err := RegisterEngine(checkinputs.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "independent"}, Shells: []string{"bash"}, ScriptInputs: sites}, routingBackend(func(_ context.Context, request checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
		names = append(names, request.Filename)
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(contract.Context{})
	if err != nil {
		t.Fatal(err)
	}
	scripts := []scriptexecution.Unit{{Run: runBody("echo run", nil), Execution: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}}
	for _, site := range sites {
		scripts = append(scripts, scriptexecution.Unit{Input: actionInput(site, "code", nil)})
	}
	if _, err := runner.Analyze(t.Context(), scriptexecution.Batch{Scripts: scripts}); err != nil {
		t.Fatal(err)
	}
	want := []string{"actionlint-script", "actionlint-input/python/owner/action/SubPath/source.py", "actionlint-input/javascript/owner/action/code.js", "actionlint-input/python/owner/action/other.py"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("guest cannot distinguish routes: got=%v want=%v", names, want)
	}
}
