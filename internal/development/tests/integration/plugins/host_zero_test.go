package plugins_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/module"
)

var (
	_ checkinputs.Checker           = (*component.Plugin)(nil)
	_ runnerapi.Provider            = (*component.Plugin)(nil)
	_ scriptengine.DescribedBackend = (*component.Plugin)(nil)
	_ scriptengine.ConfigValidator  = (*component.Plugin)(nil)
	_ checkinputs.ToolIntegrator    = (*component.Plugin)(nil)
	_ scriptengine.DescribedBackend = (*module.Engine)(nil)
	_ scriptengine.ConfigValidator  = (*module.Engine)(nil)
)

func TestHostZeroValueGetters(t *testing.T) {
	t.Parallel()
	var zeroPlugin component.Plugin
	if zeroPlugin.HasAnalyzer() || zeroPlugin.HasRunnerProvider() || zeroPlugin.HasScriptEngine() || zeroPlugin.Path() != "" || zeroPlugin.ArtifactDigest() != "" || zeroPlugin.Limits() != (component.Limits{}) {
		t.Fatal("zero component advertises loaded state")
	}
	if zeroPlugin.Descriptor().Name != "" || zeroPlugin.RunnerDescriptor().Name != "" || zeroPlugin.ScriptDescriptor().ID != "" || zeroPlugin.ToolIntegration() != nil {
		t.Fatal("zero component advertises a role descriptor")
	}
	var engine module.Engine
	if engine.Path() != "" || engine.CheckVersion() != 0 || engine.Limits() != (module.Limits{}) || engine.Metadata().ID != "" || engine.ScriptDescriptor().ID != "" {
		t.Fatal("zero engine advertises loaded state")
	}
}
