package component

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

func TestUnavailableRolesRejectCallsWithoutRuntime(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	if p.HasAnalyzer() || p.HasRunnerProvider() || p.HasScriptEngine() {
		t.Fatal("component without validated descriptors advertised roles")
	}
	if !reflect.DeepEqual(p.Descriptor(), inputs.Descriptor{}) ||
		!reflect.DeepEqual(p.RunnerDescriptor(), runnerapi.Descriptor{}) ||
		!reflect.DeepEqual(p.ScriptDescriptor(), inputs.ScriptEngineDescriptor{}) {
		t.Fatal("absent role did not return an empty descriptor")
	}
	if _, err := p.CheckDocument(t.Context(), inputs.AnalyzerInput{}); err == nil {
		t.Fatal("unavailable analyzer accepted a call")
	}
	if _, err := p.ResolveRunners(t.Context(), runnerapi.Request{}); err == nil {
		t.Fatal("unavailable runner provider accepted a call")
	}
	if _, err := p.Check(t.Context(), inputs.ScriptRequest{}); err == nil {
		t.Fatal("unavailable script engine accepted a check")
	}
	if err := p.ValidateConfig(t.Context(), nil); err == nil {
		t.Fatal("unavailable script engine accepted config validation")
	}
}
