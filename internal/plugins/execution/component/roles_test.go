package component

import (
	"reflect"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestUnavailableRolesRejectCallsWithoutRuntime(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	if p.HasAnalyzer() || p.HasRunnerProvider() || p.HasScriptEngine() {
		t.Fatal("component without validated descriptors advertised roles")
	}
	if !reflect.DeepEqual(p.Descriptor(), checkinputs.Descriptor{}) ||
		!reflect.DeepEqual(p.RunnerDescriptor(), runner.Descriptor{}) ||
		!reflect.DeepEqual(p.ScriptDescriptor(), checkinputs.ScriptEngineDescriptor{}) {
		t.Fatal("absent role did not return an empty descriptor")
	}
	if _, err := p.CheckDocument(t.Context(), checkinputs.Input{}); err == nil {
		t.Fatal("unavailable analyzer accepted a call")
	}
	if _, err := p.ResolveRunners(t.Context(), runner.Request{}); err == nil {
		t.Fatal("unavailable runner provider accepted a call")
	}
	if _, err := p.Check(t.Context(), checkinputs.ScriptRequest{}); err == nil {
		t.Fatal("unavailable script engine accepted a check")
	}
	if err := p.ValidateConfig(t.Context(), nil); err == nil {
		t.Fatal("unavailable script engine accepted config validation")
	}
}
