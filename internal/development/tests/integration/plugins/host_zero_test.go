package plugins_test

import (
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	componenthost "actionlint.kjanat.dev/v2/internal/plugins/execution/component"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/execution/module"
)

var (
	_ checkinputs.Checker                = (*componenthost.Plugin)(nil)
	_ runner.Provider                    = (*componenthost.Plugin)(nil)
	_ checkinputs.DescribedScriptBackend = (*componenthost.Plugin)(nil)
	_ checkinputs.ScriptConfigValidator  = (*componenthost.Plugin)(nil)
	_ checkinputs.ToolIntegrator         = (*componenthost.Plugin)(nil)
	_ checkinputs.DescribedScriptBackend = (*corewasm.Engine)(nil)
	_ checkinputs.ScriptConfigValidator  = (*corewasm.Engine)(nil)
)

func TestHostZeroValueGetters(t *testing.T) {
	t.Parallel()
	var component componenthost.Plugin
	if component.HasAnalyzer() || component.HasRunnerProvider() || component.HasScriptEngine() || component.Path() != "" || component.ArtifactDigest() != "" || component.Limits() != (componenthost.Limits{}) {
		t.Fatal("zero component advertises loaded state")
	}
	if component.Descriptor().Name != "" || component.RunnerDescriptor().Name != "" || component.ScriptDescriptor().ID != "" || component.ToolIntegration() != nil {
		t.Fatal("zero component advertises a role descriptor")
	}
	var engine corewasm.Engine
	if engine.Path() != "" || engine.CheckVersion() != 0 || engine.Limits() != (corewasm.Limits{}) || engine.Metadata().ID != "" || engine.ScriptDescriptor().ID != "" {
		t.Fatal("zero engine advertises loaded state")
	}
}
