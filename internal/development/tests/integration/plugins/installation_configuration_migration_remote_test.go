package plugins_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestConfigMigrationRejectsMixedRemoteSnapshots(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, "actionlint.yaml")
	remote := fixtureregistry.FixtureRegistryURL + "base.yaml"
	original := "extends: [" + remote + "]\nconfig-variables: [LOCAL]\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	registry.SetFileSequence(remote, []byte("plugins: []"), []byte("plugins: [github.com/example/plugin@v1.0.0]"))
	if _, err := plugininstall.PrepareConfigurationMigration(t.Context(), path, plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules(), Acquisition: registryAcquisition(registry)}); err == nil || !strings.Contains(err.Error(), "changed during migration preparation") {
		t.Fatalf("mixed remote snapshots accepted: %v", err)
	}
	if requests := registry.Requests(); len(requests) != 2 || requests[0] != remote || requests[1] != remote {
		t.Fatalf("snapshot comparison requests = %v", requests)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("failed preview changed config: %q, %v", content, err)
	}
}
