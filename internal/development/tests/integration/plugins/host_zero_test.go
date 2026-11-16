package plugins_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	componenthost "actionlint.kjanat.dev/v2/internal/plugins/execution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/module"
)

var (
	_ inputs.Checker                = (*componenthost.Plugin)(nil)
	_ runnerapi.Provider            = (*componenthost.Plugin)(nil)
	_ inputs.DescribedScriptBackend = (*componenthost.Plugin)(nil)
	_ inputs.ScriptConfigValidator  = (*componenthost.Plugin)(nil)
	_ inputs.ToolIntegrator         = (*componenthost.Plugin)(nil)
	_ inputs.DescribedScriptBackend = (*module.Engine)(nil)
	_ inputs.ScriptConfigValidator  = (*module.Engine)(nil)
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
	var engine module.Engine
	if engine.Path() != "" || engine.CheckVersion() != 0 || engine.Limits() != (module.Limits{}) || engine.Metadata().ID != "" || engine.ScriptDescriptor().ID != "" {
		t.Fatal("zero engine advertises loaded state")
	}
}
