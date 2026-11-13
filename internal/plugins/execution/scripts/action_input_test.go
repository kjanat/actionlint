package scripts

import (
	"fmt"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
)

func TestInputRegistrationRetainsPerEngineLimit(t *testing.T) {
	t.Parallel()
	inputs := make([]checkinputs.ScriptInput, 257)
	for i := range inputs {
		inputs[i] = checkinputs.ScriptInput{Action: "owner/action", Input: fmt.Sprintf("input-%d", i), Language: "javascript"}
	}
	descriptor := checkinputs.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "input"}, ScriptInputs: inputs}
	if _, err := RegisterEngine(descriptor, &selectorBackend{}); err == nil {
		t.Fatal("oversized per-engine catalogue accepted")
	}
}

func TestInputRegistrationUsesImmutableTypedSelectors(t *testing.T) {
	t.Parallel()
	site := checkinputs.ScriptInput{Action: "owner/repository", Input: "source", Language: "javascript"}
	descriptor := checkinputs.ScriptEngineDescriptor{Rule: rule.Descriptor{Name: "input"}, ScriptInputs: []checkinputs.ScriptInput{site}}
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
	for _, candidate := range []checkinputs.ScriptInput{
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
