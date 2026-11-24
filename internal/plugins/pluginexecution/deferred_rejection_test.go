package pluginexecution

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestDeferredScriptEngineRejectsBytesAndReselects(t *testing.T) {
	t.Parallel()
	broken := "https://" + fixtureregistry.FixturePluginName + "/releases/download/v2.0.0/plugin.wasm"
	for _, older := range []bool{true, false} {
		t.Run(fmt.Sprintf("older=%t", older), func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			if older {
				registry.Add(t, "v1.0.0", fixtureregistry.AnalyzerComponent())
			}
			registry.Add(t, "v2.0.0", wasmcomponent.Build(nil, []string{component.AnalyzerInterface}, nil))
			acquisition := dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, acquisition)
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
			if err != nil || pin.Version != "v2.0.0" || slices.Contains(registry.Requests(), broken) {
				t.Fatalf("selection=%+v error=%v requests=%v", pin, err, registry.Requests())
			}
			engine := &deferredScriptEngine{gate: make(chan struct{}, 1), pin: pin, acquisition: acquisition}
			ctx, session := network.WithSession(t.Context(), network.Options{})
			for range 2 {
				if diagnostics, err := engine.Check(ctx, scriptengine.Request{}); err != nil || diagnostics != nil {
					t.Fatalf("skipped engine: diagnostics=%v error=%v", diagnostics, err)
				}
			}
			if err := engine.ValidateConfig(ctx, nil); err != nil {
				t.Fatalf("skipped engine configuration: %v", err)
			}
			warnings := session.Warnings()
			if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "Skipping default plugin: ") || !strings.Contains(warnings[0], "incompatible analyzer contract") {
				t.Fatalf("warnings=%q", warnings)
			}
			if downloads := slices.DeleteFunc(registry.Requests(), func(url string) bool { return url != broken }); len(downloads) != 1 {
				t.Fatalf("rejected release downloads=%v", downloads)
			}
			registry.ResetRequests()
			next, err := client.ResolveCached(t.Context(), fixtureregistry.FixturePluginName, "latest")
			if older && (err != nil || next.Version != "v1.0.0") || !older && !errors.Is(err, pluginregistry.ErrNoCompatibleRelease) || slices.Contains(registry.Requests(), broken) {
				t.Fatalf("next run: selection=%+v error=%v requests=%v", next, err, registry.Requests())
			}
		})
	}
}
