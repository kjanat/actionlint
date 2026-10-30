package localapp

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestAutomaticMigrationHonorsOverlayBeforeDownloads(t *testing.T) {
	for _, input := range []string{"plugins: []", "plugins: null", "tools: {shellcheck: false, ruff: false}"} {
		for _, explicit := range []bool{false, true} {
			t.Run(input+map[bool]string{false: "/discovered", true: "/explicit"}[explicit], func(t *testing.T) {
				registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
				registry.Offline = true
				root := pluginRepository(t)
				path := filepath.Join(root, ".github", "actionlint.yaml")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				writeManifestTest(t, path, "config-variables: []\n")
				overlay, err := config.ParseOverlay("config", []byte(input))
				if err != nil {
					t.Fatal(err)
				}
				selection := PluginSelection{Overlays: []config.Overlay{overlay}}
				if explicit {
					selection.ConfigFile = path
				}
				if err := selection.Migrate(t.Context(), root, application.Input{Kind: application.Repository}, DefaultPluginOptions{Shellcheck: true, Ruff: true}); err != nil {
					t.Fatal(err)
				}
				if len(registry.Requests) != 0 {
					t.Fatalf("disabled defaults contacted registry: %v", registry.Requests)
				}
				cfg, err := config.Load(path)
				if err != nil || cfg.ConfigVariables == nil || cfg.Plugins == nil || len(cfg.Plugins) != 0 {
					t.Fatalf("migration ignored opt-out: %+v, %v", cfg, err)
				}
				lock, err := pluginregistry.OpenLock(t.Context(), filepath.Join(filepath.Dir(path), pluginregistry.LockName))
				if err != nil || len(lock.Lock.Plugins) != 0 {
					t.Fatalf("disabled default pins: %+v, %v", lock, err)
				}
			})
		}
	}
}
