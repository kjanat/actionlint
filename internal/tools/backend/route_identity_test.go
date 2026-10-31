package backend

import (
	"context"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/rule"
)

func TestInputRequestsIdentifyLanguageAndSite(t *testing.T) {
	sites := []analyzer.ScriptInput{{Action: "owner/action/SubPath", Input: "source", Language: "python"}, {Action: "owner/action", Input: "code", Language: "javascript"}, {Action: "owner/action", Input: "other", Language: "python"}}
	var names []string
	registration, err := RegisterEngine(analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "independent"}, Shells: []string{"bash"}, ScriptInputs: sites}, routingBackend(func(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
		names = append(names, request.Filename)
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	scripts := []tools.Script{{Text: "echo run", Execution: tools.Execution{Shell: "bash"}}}
	for _, site := range sites {
		scripts = append(scripts, tools.Script{Input: &site, Text: "code"})
	}
	if _, err := runner.Analyze(t.Context(), tools.Batch{Scripts: scripts}); err != nil {
		t.Fatal(err)
	}
	want := []string{"actionlint-script", "actionlint-input/python/owner/action/SubPath/source.py", "actionlint-input/javascript/owner/action/code.js", "actionlint-input/python/owner/action/other.py"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("guest cannot distinguish routes: got=%v want=%v", names, want)
	}
}
