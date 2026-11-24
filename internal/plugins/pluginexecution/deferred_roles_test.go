package pluginexecution

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func TestDeferredEngineRequiresScriptOnlyRoles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		roles component.Roles
		want  bool
	}{
		{component.Roles{ScriptEngine: component.ScriptEngineInterface}, true},
		{component.Roles{ScriptEngine: component.ScriptEngine03Interface, TemplateEffects: component.TemplateEffects03Interface}, true},
		{component.Roles{ScriptEngine: component.ScriptEngine03Interface, Analyzer: component.AnalyzerInterface}, false},
		{component.Roles{ScriptEngine: component.ScriptEngine03Interface, Runner: component.Runner03Interface}, false},
		{component.Roles{Analyzer: component.AnalyzerInterface}, false},
	} {
		if got := tc.roles.ScriptOnly(); got != tc.want {
			t.Errorf("%+v.ScriptOnly() = %t", tc.roles, got)
		}
	}
}

func TestDeferredEngineRejectsUndeclaredRolesFromBytes(t *testing.T) {
	t.Parallel()
	fixture := newDeferredReselection(t, fixtureregistry.AnalyzerComponent())
	ctx, session := network.WithSession(t.Context(), network.Options{})
	checkSkipped(ctx, t, fixture.engine, session, "exports roles not declared by its script-only metadata")
	next, err := fixture.client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || next.Version != "v1.0.0" {
		t.Fatalf("next run: selection=%+v error=%v", next, err)
	}
}
