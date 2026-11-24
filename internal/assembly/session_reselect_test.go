package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestSessionRoutesByReselectedDescriptor(t *testing.T) {
	t.Parallel()
	script, err := os.ReadFile("../plugins/pluginexecution/component/testdata/plugins/ruff-action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../plugins/pluginexecution/component/testdata/sources/script-engine/ruff-action-input.metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	execute := checkinputs.ScriptInput{Action: "example/execute", Input: "code", Language: "javascript"}
	metadata := func(version string, inputs int) []byte {
		var declared, capabilities map[string]json.RawMessage
		var scriptInputs []json.RawMessage
		if err := json.Unmarshal(source, &declared); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(declared["capabilities"], &capabilities); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(capabilities["script_inputs"], &scriptInputs); err != nil {
			t.Fatal(err)
		}
		encode := func(value any) json.RawMessage {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		capabilities["script_inputs"] = encode(scriptInputs[:inputs])
		declared["capabilities"] = encode(capabilities)
		declared["repository"] = encode("https://github.com/publisher/engine")
		declared["plugin_version"] = encode(strings.TrimPrefix(version, "v"))
		return encode(declared)
	}
	catalogue := fixtureregistry.NewPluginRegistry(t)
	publisher := fixtureregistry.PluginPublisher{PluginID: "echo-component", BaseURL: pluginregistry.DefaultURL, Name: "github.com/publisher/engine", Aliases: []string{"ruff"}, RepositoryID: 99, Metadata: metadata("v1.0.0", 2)}
	catalogue.AddPlugin(t, publisher, "v1.0.0", script)
	publisher.Metadata = metadata("v2.0.0", 1)
	catalogue.AddPlugin(t, publisher, "v2.0.0", wasmcomponent.Build(nil, []string{component.ScriptEngineInterface}, nil))
	catalogue.PublishPlugin(t, publisher, "v1.0.0", "v2.0.0")
	root := t.TempDir()
	path := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, session := network.WithSession(t.Context(), network.Options{})
	selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{"ruff"}, Acquisition: dependencycache.Acquisition{UserCacheDir: catalogue.UserCacheDir(), Connections: catalogue.Connections()}}
	sources, err := selection.Resolve(ctx, root, checkrequest.Request{Kind: checkrequest.Repository})
	if err != nil || sources.Releases == nil || len(sources.Releases.Plugins) != 1 || len(sources.LazySources) != 1 || sources.Releases.Plugins[0].Version != "v2.0.0" {
		t.Fatalf("sources=%+v error=%v", sources, err)
	}
	opened, err := Open(ctx, checking.Options{Tools: registry.Registrations(nil)}, sources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := opened.Close(t.Context()); err != nil {
			t.Error(err)
		}
	})
	routes := func(options checking.Options) []checkinputs.ScriptInput {
		index := slices.IndexFunc(options.Tools, func(registration contract.Registration) bool { return registration.Name == "ruff" })
		if index < 0 {
			t.Fatalf("no ruff tool in %+v", options.Tools)
		}
		return options.Tools[index].ScriptInputs
	}
	if slices.Contains(routes(opened.Options), execute) {
		t.Fatalf("v2 routes=%+v", routes(opened.Options))
	}
	if len(opened.plugins.ScriptEngines) != 1 {
		t.Fatalf("script engines=%+v", opened.plugins.ScriptEngines)
	}
	if diagnostics, err := opened.plugins.ScriptEngines[0].Backend.Check(ctx, scriptengine.Request{}); err != nil || diagnostics != nil {
		t.Fatalf("rejected release: diagnostics=%v error=%v", diagnostics, err)
	}
	if warnings := session.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], publisher.Name+"@v2.0.0 does not load") {
		t.Fatalf("warnings=%q", warnings)
	}
	options, err := opened.BeginAnalysis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(routes(options), execute) || !slices.Contains(routes(opened.Options), execute) {
		t.Fatalf("v1 routes=%+v", routes(options))
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := opened.BeginAnalysis(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled analysis error=%v", err)
	}
	invalid := opened.plugins.ScriptEngines[0]
	invalid.Descriptor.Shells = []string{"two words"}
	if _, err := refreshScriptEngine(opened.Options.Tools, opened.tools, invalid); err == nil || !strings.Contains(err.Error(), `invalid script shell selector "two words"`) {
		t.Fatalf("invalid routes error=%v", err)
	}
}
