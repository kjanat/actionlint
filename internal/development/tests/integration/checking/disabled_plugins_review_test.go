package checking_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	pluginexec "actionlint.kjanat.dev/v2/internal/plugins/execution"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestDisabledPluginsDoNotPreventBuiltinFindings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		direct, offline, all bool
	}{
		{"cold registry no tools", false, false, true},
		{"cold direct offline no tools", true, true, true},
		{"canonical disabled declaration", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := repositoryfixture.Directory(t)
			registry := fixtureregistry.NewPluginRegistry(t)
			acquisition := pluginacquisition.Settings{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			configPath := filepath.Join(root, ".github", "actionlint.yaml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
				t.Fatal(err)
			}
			declaration := fmt.Sprintf("plugins: [{name: %s, version: v1.0.0, registry: %s}]\n", fixtureregistry.FixturePluginName, fixtureregistry.FixtureRegistryURL)
			if tc.direct {
				declaration = fmt.Sprintf("plugins: [https://plugins.example/ruff.wasm#sha256=%s]\n", strings.Repeat("a", 64))
			}
			fixturefiles.WriteText(t, configPath, declaration+"repository: {runners: []}\n")
			workflow := filepath.Join(root, ".github", "workflows", "ci.yml")
			if err := os.MkdirAll(filepath.Dir(workflow), 0700); err != nil {
				t.Fatal(err)
			}
			fixturefiles.WriteText(t, workflow, "on: push\njobs:\n  test:\n    runs-on: unknown-runner\n    steps:\n      - run: echo hi\npermissions: {}\n")
			selection := pluginselection.Selection{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition, DisableTools: tc.all}
			if !tc.direct {
				var disabled []string
				if !tc.all {
					disabled = []string{fixtureregistry.FixturePluginName}
				}
				selection = pluginselection.WithTools(selection, disabled, []string{fixtureregistry.FixturePluginName})
			}
			ctx := t.Context()
			if tc.offline {
				ctx, _ = network.WithSession(ctx, network.Options{Offline: true})
			}
			result, err := checking.CheckProjects(ctx, checking.Options{WorkingDir: root}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{workflow}}, selection)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if strings.Contains(diagnostic.Message, "unknown-runner") {
					found = true
				}
			}
			if !found {
				t.Fatalf("built-in finding missing: %+v", result.Diagnostics)
			}
			if requests := registry.Requests(); len(requests) != 0 {
				t.Fatalf("unexpected requests: %v", requests)
			}
		})
	}
}

func TestNoToolsRetainsAvailableRunnerComponents(t *testing.T) {
	t.Parallel()
	component, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", component)
	acquisition := pluginacquisition.Settings{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
	client, err := pluginregistry.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := pluginregistry.Sync(t.Context(), pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{pin}}, acquisition); err != nil {
		t.Fatal(err)
	}
	root := repositoryfixture.Directory(t)
	configPath := filepath.Join(root, "config.yaml")
	fixturefiles.WriteText(t, configPath, fmt.Sprintf("plugins: [{name: %s, version: v1.0.0, registry: %s}]\n", fixtureregistry.FixturePluginName, fixtureregistry.FixtureRegistryURL))
	registry.ResetRequests()
	for _, selection := range []pluginselection.Selection{
		{RuleSet: checking.ConfigurationRules(), ConfigFile: configPath, Acquisition: acquisition, DisableTools: true},
		{RuleSet: checking.ConfigurationRules(), Paths: []string{filepath.Join(root, "runner.wasm")}, Acquisition: acquisition, DisableTools: true},
	} {
		fixturefiles.WriteText(t, filepath.Join(root, "runner.wasm"), string(component))
		sources, err := selection.Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := pluginexec.LoadPlugins(t.Context(), sources, checking.BuiltinRuleNames(), os.Stderr, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.RunnerProviders()) != 1 {
			t.Errorf("available runner provider missing: %+v", loaded.RunnerProviders())
		}
		if err := loaded.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	cachePath := filepath.Join(registry.UserCacheDir(), "actionlint", "artifacts", pin.SHA256)
	if err := os.WriteFile(cachePath, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = (pluginselection.Selection{RuleSet: checking.ConfigurationRules(), ConfigFile: configPath, Acquisition: acquisition, DisableTools: true}).Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("corrupt cached artifact was ignored: %v", err)
	}
	if requests := registry.Requests(); len(requests) != 0 {
		t.Fatalf("unexpected requests: %v", requests)
	}
}
