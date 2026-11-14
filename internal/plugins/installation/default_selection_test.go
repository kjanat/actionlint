package installation_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	installation "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestDefaultPluginsUseRegistryAndOfflineCache(t *testing.T) {
	t.Parallel()
	for _, tool := range []string{"shellcheck", "ruff"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: "test-engine", BaseURL: installation.DefaultURL, Name: "github.com/publisher/engine", Aliases: []string{tool}, RepositoryID: 99}, "v1.0.0", emptyComponent)
			root := t.TempDir()
			path := filepath.Join(root, "actionlint.yaml")
			if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			selection := installation.Selection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{tool}, Acquisition: installation.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}}
			selected, err := selection.Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
			if err != nil {
				t.Fatal(err)
			}
			if selected.Releases == nil || len(selected.Releases.Plugins) != 1 || selected.Releases.Plugins[0].Name != "github.com/publisher/engine" || selected.Releases.Plugins[0].Version != "v1.0.0" || len(selected.Artifacts) != 1 {
				t.Fatalf("selected registry artifacts=%+v", selected)
			}
			requests := len(registry.Requests())
			reused, err := selection.Resolve(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository})
			if err != nil || len(reused.Artifacts) != 1 || len(registry.Requests()) != requests {
				t.Fatalf("repeated online selection: artifacts=%v error=%v requests=%v", reused.Artifacts, err, registry.Requests())
			}
			registry.SetOffline(true)
			ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true})
			for _, tc := range []struct {
				name     string
				config   string
				disabled bool
				input    inputmodel.Request
			}{
				{"explicit config", path, false, inputmodel.Request{Kind: inputmodel.Stdin}},
				{"no config", "", true, inputmodel.Request{Kind: inputmodel.Stdin}},
				{"standalone file", "", false, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{filepath.Join(root, "workflow.yml")}}},
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
		{"disabled rule", "lint: {rules: {external: {shellcheck: off}}}", nil, false},
		{"native backend", "tools: {shellcheck: {backend: {process: {executable: custom-shellcheck}}}}", nil, false},
		{"disabled by invocation", "{}", []string{"shellcheck"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "actionlint.yaml")
			if err := os.WriteFile(path, []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, _ := network.WithSession(t.Context(), network.Options{Offline: true, CacheDirectory: t.TempDir()})
			selection := installation.Selection{RuleSet: checking.ConfigurationRules(), ConfigFile: path, DefaultTools: []string{"shellcheck"}, DisabledTools: tc.disabled, Acquisition: installation.Acquisition{UserCacheDir: t.TempDir()}}
			selected, err := selection.Resolve(ctx, root, inputmodel.Request{Kind: inputmodel.Repository})
			if errors.Is(err, network.ErrOffline) != tc.wantMissing {
				t.Fatalf("selection error=%v, want missing offline plugin=%t", err, tc.wantMissing)
			}
			if !tc.wantMissing && (err != nil || len(selected.Paths) != 0) {
				t.Fatalf("unexpected plugin selection: paths=%v error=%v", selected.Paths, err)
			}
		})
	}
}
