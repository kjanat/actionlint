package component

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/runner"
)

func TestUnavailableRolesRejectCallsWithoutRuntime(t *testing.T) {
	p := &Plugin{}
	if p.HasAnalyzer() || p.HasRunnerProvider() || p.HasScriptEngine() {
		t.Fatal("component without validated descriptors advertised roles")
	}
	if !reflect.DeepEqual(p.Descriptor(), analyzer.Descriptor{}) ||
		!reflect.DeepEqual(p.RunnerDescriptor(), runner.Descriptor{}) ||
		!reflect.DeepEqual(p.ScriptDescriptor(), analyzer.ScriptEngineDescriptor{}) {
		t.Fatal("absent role did not return an empty descriptor")
	}
	if _, err := p.Analyze(t.Context(), analyzer.Input{}); err == nil {
		t.Fatal("unavailable analyzer accepted a call")
	}
	if _, err := p.ResolveRunners(t.Context(), runner.Request{}); err == nil {
		t.Fatal("unavailable runner provider accepted a call")
	}
	if _, err := p.Check(t.Context(), analyzer.ScriptRequest{}); err == nil {
		t.Fatal("unavailable script engine accepted a check")
	}
	if err := p.ValidateConfig(t.Context(), nil); err == nil {
		t.Fatal("unavailable script engine accepted config validation")
	}
}
