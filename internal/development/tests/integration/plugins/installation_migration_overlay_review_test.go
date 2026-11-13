package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestAutomaticMigrationHonorsOverlayBeforeDownloads(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"plugins: []", "plugins: null", "tools: {shellcheck: false, ruff: false}"} {
		for _, explicit := range []bool{false, true} {
			t.Run(input+map[bool]string{false: "/discovered", true: "/explicit"}[explicit], func(t *testing.T) {
				t.Parallel()
				registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
				acquisition := registryAcquisition(registry)
				registry.SetOffline(true)
				root := repositoryfixture.Directory(t)
				path := filepath.Join(root, ".github", "actionlint.yaml")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				fixturefiles.WriteText(t, path, "config-variables: []\n")
				overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte(input))
				if err != nil {
					t.Fatal(err)
				}
				selection := plugininstall.Selection{Acquisition: acquisition, Overlays: []config.Overlay{overlay}, RuleSet: checking.ConfigurationRules()}
				if explicit {
					selection.ConfigFile = path
				}
				if err := selection.Migrate(t.Context(), root, inputmodel.Request{Kind: inputmodel.Repository}, plugininstall.DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}, RuleSet: checking.ConfigurationRules()}); err != nil {
					t.Fatal(err)
				}
				if len(registry.Requests()) != 0 {
					t.Fatalf("disabled defaults contacted registry: %v", registry.Requests())
				}
				cfg, err := checking.ConfigurationRules().Load(path)
				if err != nil || config.VariableNames(cfg.Facts()) == nil || cfg.Plugins == nil || len(cfg.Plugins) != 0 {
					t.Fatalf("migration ignored opt-out: %+v, %v", cfg, err)
				}
				lock, err := plugininstall.OpenLock(t.Context(), filepath.Join(filepath.Dir(path), plugininstall.LockName))
				if err != nil || len(lock.Lock.Plugins) != 0 {
					t.Fatalf("disabled default pins: %+v, %v", lock, err)
				}
			})
		}
	}
}
