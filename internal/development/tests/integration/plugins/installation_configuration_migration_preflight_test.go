package plugins_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestAutomaticMigrationPreflightsAllRepositories(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	acquisition := registryAcquisition(registry)
	registry.SetOffline(true)
	first := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	second := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	firstConfig := filepath.Join(first, ".github", "actionlint.yaml")
	secondConfig := filepath.Join(second, ".github", "actionlint.yaml")
	input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{
		filepath.Join(first, ".github", "workflows", "first.yml"),
		filepath.Join(second, ".github", "workflows", "second.yml"),
	}}
	err := (plugininstall.Selection{Acquisition: acquisition, RuleSet: checking.ConfigurationRules()}).Migrate(t.Context(), first, input, plugininstall.DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}, RuleSet: checking.ConfigurationRules()})
	if err == nil || !strings.Contains(err.Error(), "inputs span repositories with plugin configuration") {
		t.Fatalf("migration error = %v, want multi-repository plugin error", err)
	}
	if len(registry.Requests()) != 0 {
		t.Fatalf("migration resolved plugins before rejecting repository selection: %v", registry.Requests())
	}
	for _, path := range []string{firstConfig, secondConfig} {
		if got := mustReadFile(t, path); !bytes.Equal(got, []byte("config-variables: []\n")) {
			t.Errorf("preflight migrated %s: %q", path, got)
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(path), "actionlint.plugins.lock.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("preflight wrote lock beside %s: %v", path, err)
		}
	}
}

func TestAutomaticMigrationAllowsMultipleRepositoriesWithoutPlugins(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	acquisition := registryAcquisition(registry)
	first := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	second := migrationRepositoryWithLegacyConfig(t, "config-secrets: []\n")
	input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{
		filepath.Join(first, ".github", "workflows", "first.yml"),
		filepath.Join(second, ".github", "workflows", "second.yml"),
	}}
	if err := (plugininstall.Selection{Acquisition: acquisition, RuleSet: checking.ConfigurationRules()}).Migrate(t.Context(), first, input, plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules()}); err != nil {
		t.Fatal(err)
	}
	selected, err := (plugininstall.Selection{Acquisition: acquisition, RuleSet: checking.ConfigurationRules()}).Select(t.Context(), first, input)
	if err != nil {
		t.Fatalf("selecting multiple plugin-free repositories: %v", err)
	}
	if len(selected.Paths) != 0 || len(registry.Requests()) != 0 {
		t.Fatalf("plugin-free selection unexpectedly activated plugins: %+v, requests %v", selected, registry.Requests())
	}
	for _, path := range []string{
		filepath.Join(first, ".github", "actionlint.yaml"),
		filepath.Join(second, ".github", "actionlint.yaml"),
	} {
		if bytes.Equal(mustReadFile(t, path), []byte("config-variables: []\n")) || bytes.Equal(mustReadFile(t, path), []byte("config-secrets: []\n")) {
			t.Errorf("legacy config was not migrated: %s", path)
		}
	}
}

func TestAutomaticMigrationPreflightsCurrentPluginRepository(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := registryAcquisition(registry)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	current := repositoryfixture.Directory(t)
	project, err := plugininstall.OpenProject(t.Context(), checking.ConfigurationRules(), acquisition, current, "")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(pin.Name, pin.Version, pin.Registry)
	if err != nil {
		t.Fatal(err)
	}
	refs := []pluginref.Reference{ref}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	legacy := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	legacyPath := filepath.Join(legacy, ".github", "actionlint.yaml")
	input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{
		filepath.Join(current, ".github", "workflows", "current.yml"),
		filepath.Join(legacy, ".github", "workflows", "legacy.yml"),
	}}
	err = (plugininstall.Selection{Acquisition: acquisition, RuleSet: checking.ConfigurationRules()}).Migrate(t.Context(), current, input, plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules()})
	if err == nil || !strings.Contains(err.Error(), "inputs span repositories with plugin configuration") {
		t.Fatalf("migration error = %v, want multi-repository plugin error", err)
	}
	if got := mustReadFile(t, legacyPath); !bytes.Equal(got, []byte("config-variables: []\n")) {
		t.Fatalf("migration changed legacy config beside active project: %q", got)
	}
}

func TestAutomaticMigrationRejectsPluginDefaultsWithUnmanagedInput(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	acquisition := registryAcquisition(registry)
	registry.SetOffline(true)
	root := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	legacyPath := filepath.Join(root, ".github", "actionlint.yaml")
	outside := t.TempDir()
	workflow := filepath.Join(outside, "workflow.yml")
	if err := os.WriteFile(workflow, []byte("on: push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{
		filepath.Join(root, ".github", "workflows", "repo.yml"),
		workflow,
	}}
	err := (plugininstall.Selection{Acquisition: acquisition, RuleSet: checking.ConfigurationRules()}).Migrate(t.Context(), root, input, plugininstall.DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}, RuleSet: checking.ConfigurationRules()})
	if err == nil || !strings.Contains(err.Error(), "inputs span repositories with plugin configuration") {
		t.Fatalf("migration error = %v, want unmanaged-input plugin error", err)
	}
	if len(registry.Requests()) != 0 {
		t.Fatalf("migration resolved plugins before rejecting unmanaged input: %v", registry.Requests())
	}
	if got := mustReadFile(t, legacyPath); !bytes.Equal(got, []byte("config-variables: []\n")) {
		t.Fatalf("migration changed config before rejecting unmanaged input: %q", got)
	}
}

func TestAutomaticMigrationDeduplicatesPhysicalRepositoryAliases(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	acquisition := registryAcquisition(registry)
	root := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	input := inputmodel.Request{Kind: inputmodel.Files, Paths: []string{
		filepath.Join(root, ".github", "workflows", "root.yml"),
		filepath.Join(alias, ".github", "workflows", "alias.yml"),
	}}
	if err := (plugininstall.Selection{Acquisition: acquisition, RuleSet: checking.ConfigurationRules()}).Migrate(t.Context(), root, input, plugininstall.DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}, RuleSet: checking.ConfigurationRules()}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github", "actionlint.yaml")
	migrated, err := checking.ConfigurationRules().Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.Plugins) != 2 {
		t.Fatalf("active defaults were not migrated through physical alias: %+v", migrated.Plugins)
	}
	if len(registry.Requests()) == 0 {
		t.Fatal("active default plugins did not resolve")
	}
}

func migrationRepositoryWithLegacyConfig(t *testing.T, contents string) string {
	t.Helper()
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
