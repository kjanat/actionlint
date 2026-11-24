package pluginexecution

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
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
	engine := &deferredScriptEngine{pin: pluginregistry.Pin{Kind: artifacts.Component}}
	for path, want := range map[string]string{
		"component/testdata/plugins/script-engine.wasm": "",
		"component/testdata/plugins/no-sudo.wasm":       "default script component exports roles not declared by its script-only metadata",
	} {
		artifact, err := artifacts.Resolve(t.Context(), path, artifacts.Options{})
		if err != nil {
			t.Fatal(err)
		}
		backend, err := engine.load(t.Context(), artifact)
		if want == "" {
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if err := backend.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error=%v, want %q", path, err, want)
		}
	}
}
