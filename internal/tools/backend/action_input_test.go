package backend

import (
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/rule"
)

func TestInputRegistrationUsesImmutableTypedSelectors(t *testing.T) {
	site := analyzer.ScriptInput{Action: "owner/repository", Input: "source", Language: "javascript"}
	descriptor := analyzer.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "input"}, ScriptInputs: []analyzer.ScriptInput{site}}
	backend := &selectorBackend{}
	registration, err := RegisterEngine(descriptor, backend)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.ScriptInputs[0].Language = "python"
	registration.ScriptInputs[0].Input = "other"
	runner, err := registration.Build(tools.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if registration.RequiresExecution {
		t.Fatal("input-only engine requires runner shell inference")
	}
	for _, candidate := range []analyzer.ScriptInput{
		site,
		{Action: site.Action, Input: site.Input, Language: "python"},
		{Action: "other/repository", Input: site.Input, Language: site.Language},
		{Action: site.Action, Input: "other", Language: site.Language},
	} {
		if _, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Input: &candidate}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runner.Analyze(t.Context(), tools.Batch{Scripts: []tools.Script{{Execution: tools.Execution{Shell: "javascript"}}}}); err != nil {
		t.Fatal(err)
	}
	if backend.calls.Load() != 1 {
		t.Fatalf("input engine checked %d unrelated snippets", backend.calls.Load())
	}
}
