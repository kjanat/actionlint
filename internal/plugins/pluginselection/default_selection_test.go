package pluginselection_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/wasmcomponent"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

var analyzerComponent = fixtureregistry.AnalyzerComponent()

func TestDefaultPluginsUseRegistryAndOfflineCache(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"shellcheck", "ruff"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "test-engine", BaseURL: pluginregistry.DefaultURL, Name: "github.com/publisher/engine", Aliases: []string{tool}, RepositoryID: 99}, "v1.0.0", analyzerComponent)
			root := t.TempDir()
			path := filepath.Join(root, "actionlint.yaml")
			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{tool}, Acquisition: dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}}
			selected, err := selection.Resolve(t.Context(), root, checkrequest.Request{Kind: checkrequest.Repository})
			if err != nil {
				t.Fatal(err)
			}
			if selected.Releases == nil || len(selected.Releases.Plugins) != 1 || selected.Releases.Plugins[0].Name != "github.com/publisher/engine" || selected.Releases.Plugins[0].Version != "v1.0.0" || len(selected.Artifacts) != 1 {
				t.Fatalf("selected registry artifacts=%+v", selected)
			}
			requests := len(registry.Requests())
			reused, err := selection.Resolve(t.Context(), root, checkrequest.Request{Kind: checkrequest.Repository})
			if err != nil || len(reused.Artifacts) != 1 || len(registry.Requests()) != requests {
				t.Fatalf("repeated online selection: artifacts=%v error=%v requests=%v", reused.Artifacts, err, registry.Requests())
			}
			registry.SetOffline(true)
			ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
			for _, tc := range []struct {
				name     string
				config   string
				disabled bool
				input    checkrequest.Request
			}{
				{"explicit config", path, false, checkrequest.Request{Kind: checkrequest.Stdin}},
				{"no config", "", true, checkrequest.Request{Kind: checkrequest.Stdin}},
				{"standalone file", "", false, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}},
			} {
				selection.ConfigFile, selection.Disabled = tc.config, tc.disabled
				cached, err := selection.Resolve(ctx, root, tc.input)
				if err != nil || len(cached.Artifacts) != 1 || len(registry.Requests()) != requests {
					t.Fatalf("%s: cached artifacts=%v error=%v requests=%v", tc.name, cached.Artifacts, err, registry.Requests())
				}
				if cached.Releases == nil || len(cached.Releases.Plugins) != 1 || cached.Releases.Plugins[0] != selected.Releases.Plugins[0] {
					t.Fatalf("%s: cached release=%+v, want %+v", tc.name, cached.Releases, selected.Releases)
				}
			}
			contents, err := os.ReadFile(path)
			if err != nil || string(contents) != "{}" {
				t.Fatalf("configuration changed: %s error=%v", contents, err)
			}
		})
	}
}

func TestDefaultPluginWithoutCompatibleReleaseIsSkipped(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "test-engine", BaseURL: pluginregistry.DefaultURL, Name: "github.com/publisher/engine", Aliases: []string{"shellcheck"}, RepositoryID: 99}, "v1.0.0", wasmcomponent.Build(nil, []string{"kjanat:actionlint/analyzer@99.0.0"}, nil))
	root := t.TempDir()
	path := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, session := network.WithSession(t.Context(), network.Options{})
	selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{"shellcheck"}, Acquisition: dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}}
	selected, err := selection.Resolve(ctx, root, checkrequest.Request{Kind: checkrequest.Repository})
	if err != nil || len(selected.Paths) != 0 || !slices.Equal(selected.SkippedTools, []string{"shellcheck"}) {
		t.Fatalf("selected=%+v error=%v", selected, err)
	}
	warnings := session.Warnings()
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "Skipping default plugin: shellcheck: "+pluginregistry.ErrNoCompatibleRelease.Error()) {
		t.Fatalf("warnings=%q", warnings)
	}
}

func TestLazyDefaultsFollowComponentExports(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		exports []string
		lazy    bool
	}{
		{"script engine", []string{component.ScriptEngine03Interface}, true},
		{"script engine and analyzer", []string{component.AnalyzerInterface, component.ScriptEngine03Interface}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			metadata := fmt.Appendf(nil, `{"metadata_version":1,"id":"test-engine","name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":"1.0.0","repository":"https://github.com/publisher/engine","transport":"wasm-component","interface":%q,"wit_sha256":%q}`, component.ScriptEngine03Interface, strings.Repeat("0", 64))
			registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "test-engine", BaseURL: pluginregistry.DefaultURL, Name: "github.com/publisher/engine", Aliases: []string{"ruff"}, RepositoryID: 99, Metadata: metadata}, "v1.0.0", wasmcomponent.Build(nil, tc.exports, nil))
			root := t.TempDir()
			path := filepath.Join(root, "actionlint.yaml")
			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{"ruff"}, Acquisition: dependencycache.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}}
			offline, _ := network.WithSession(t.Context(), network.Options{Offline: true})
			for _, ctx := range []context.Context{t.Context(), offline} {
				selected, err := selection.Resolve(ctx, root, checkrequest.Request{Kind: checkrequest.Repository})
				if err != nil || selected.Releases == nil || len(selected.Releases.Plugins) != 1 {
					t.Fatalf("selected=%+v error=%v", selected, err)
				}
				pin := selected.Releases.Plugins[0]
				if !strings.HasPrefix(pin.Contract, component.ScriptEngine03Interface) {
					t.Fatalf("contract=%s", pin.Contract)
				}
				if _, lazy := selected.LazySources[pin.Source()]; lazy != tc.lazy {
					t.Fatalf("roles=%+v lazy=%t, want %t", pin.Roles, lazy, tc.lazy)
				}
			}
		})
	}
}

func TestDefaultWasmPluginSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config string
		disabled     []string
		wantMissing  bool
	}{
		{"omitted plugins", "{}", nil, true},
		{"empty plugins", "plugins: []", nil, false},
		{"disabled tool", "tools: {shellcheck: false}", nil, false},
		{"override re-enables tool", "tools: {shellcheck: false}\noverrides: [{includes: [python.yml], tools: {shellcheck: true}}]", nil, true},
		{"disabled rule", "lint: {rules: {external: {shellcheck: off}}}", nil, false},
		{"disabled rule with unrelated malformed setting", "lint: {rules: {external: {shellcheck: off}, correctness: {expression: {level: [invalid]}}}}", nil, false},
		{"disabled lint", "lint: {enabled: false}", nil, false},
		{"disabled group", "lint: {rules: {external: off}}", nil, false},
		{"native backend", "tools: {shellcheck: {backend: {process: {executable: custom-shellcheck}}}}", nil, false},
		{"disabled by invocation", "{}", []string{"shellcheck"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "actionlint.yaml")
			if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true, CacheDirectory: t.TempDir()})
			selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{"shellcheck"}, DisabledTools: tc.disabled, Acquisition: dependencycache.Acquisition{UserCacheDir: t.TempDir()}}
			selected, err := selection.ResolveForRules(ctx, root)
			if errors.Is(err, network.ErrOffline) != tc.wantMissing {
				t.Fatalf("selection error=%v, want missing offline plugin=%t", err, tc.wantMissing)
			}
			if !tc.wantMissing && (err != nil || len(selected.Paths) != 0) {
				t.Fatalf("unexpected plugin selection: paths=%v error=%v", selected.Paths, err)
			}
		})
	}
}

func TestResolveForRulesHonorsInheritedDefaultSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := filepath.Join(root, "base.yml")
	leaf := filepath.Join(root, "actionlint.yml")
	if err := os.WriteFile(base, []byte("tools: {shellcheck: false}\nlint: {rules: {correctness: {expression: {level: [invalid]}}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("extends: [base.yml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true, CacheDirectory: t.TempDir()})
	selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: leaf, DefaultTools: []string{"shellcheck"}, Acquisition: dependencycache.Acquisition{UserCacheDir: t.TempDir()}}
	selected, err := selection.ResolveForRules(ctx, root)
	if err != nil || len(selected.Paths) != 0 {
		t.Fatalf("inherited disabled tool: paths=%v error=%v", selected.Paths, err)
	}
}
