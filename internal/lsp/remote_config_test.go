package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestEditorRemoteConfigUsesSessionConnections(t *testing.T) {
	t.Parallel()
	for _, explicit := range []bool{false, true} {
		name := "acquisition"
		if explicit {
			name = "options"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			const remote = "https://config.example/base.yaml"
			registry.SetFile(remote, []byte("lint: {rules: {disable: [empty-run]}}\n"))
			root := t.TempDir()
			config := filepath.Join(root, "actionlint.yaml")
			if err := os.WriteFile(config, []byte("extends: ['"+remote+"']\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "workflow.yml")
			if err := os.WriteFile(path, []byte("on: push\npermissions: {}\njobs: {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			acquisition := plugininstall.Acquisition{UserCacheDir: registry.UserCacheDir(), Connections: registry.Connections()}
			options := checking.Options{WorkingDir: root, ConfigFile: config}
			sources := plugininstall.Sources{Acquisition: acquisition}
			if explicit {
				options.ConfigConnections = registry.Connections()
				sources.Acquisition = plugininstall.Acquisition{}
			}
			session, err := newEditorSources(t.Context(), options, sources)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := session.close(context.WithoutCancel(t.Context())); err != nil {
					t.Error(err)
				}
			})
			documents := map[string]Document{path: {URI: FileURI(path), Version: 1, Text: "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ''\n"}}
			result, err := session.analyze(t.Context(), documents)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "empty-run" {
					t.Fatalf("remote configuration did not disable empty-run: %+v", result.Diagnostics)
				}
			}
			if requests := registry.Requests(); len(requests) != 1 || requests[0] != remote {
				t.Fatalf("config requests = %v, want [%s]", requests, remote)
			}
			registry.SetFile(remote, []byte("{}\n"))
			result, err = session.analyze(t.Context(), documents)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "empty-run" {
					found = true
				}
			}
			if !found {
				t.Fatalf("updated remote configuration or unsaved buffer ignored: %+v", result.Diagnostics)
			}
			if requests := registry.Requests(); len(requests) != 2 || requests[1] != remote {
				t.Fatalf("second analysis requests = %v, want two requests for %s", requests, remote)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := session.analyze(ctx, documents); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled analysis: %v", err)
			}
			if requests := registry.Requests(); len(requests) != 2 {
				t.Fatalf("canceled analysis made a request: %v", requests)
			}
		})
	}
}
