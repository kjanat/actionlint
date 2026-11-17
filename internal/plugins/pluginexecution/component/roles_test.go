package component

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

func TestUnavailableRolesRejectCallsWithoutRuntime(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	if p.HasAnalyzer() || p.HasRunnerProvider() || p.HasScriptEngine() {
		t.Fatal("component without validated descriptors advertised roles")
	}
	if !reflect.DeepEqual(p.Descriptor(), checkinputs.Descriptor{}) ||
		!reflect.DeepEqual(p.RunnerDescriptor(), runnerapi.Descriptor{}) ||
		!reflect.DeepEqual(p.ScriptDescriptor(), scriptengine.Descriptor{}) {
		t.Fatal("absent role did not return an empty descriptor")
	}
	if _, err := p.CheckDocument(t.Context(), checkinputs.AnalyzerInput{}); err == nil {
		t.Fatal("unavailable analyzer accepted a call")
	}
	if _, err := p.ResolveRunners(t.Context(), runnerapi.Request{}); err == nil {
		t.Fatal("unavailable runner provider accepted a call")
	}
	if _, err := p.Check(t.Context(), scriptengine.Request{}); err == nil {
		t.Fatal("unavailable script engine accepted a check")
	}
	if err := p.ValidateConfig(t.Context(), nil); err == nil {
		t.Fatal("unavailable script engine accepted config validation")
	}
}
