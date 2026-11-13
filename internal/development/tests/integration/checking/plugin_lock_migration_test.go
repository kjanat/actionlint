package checking_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func legacyRegistryProject(t *testing.T) (*fixtureregistry.PluginRegistry, string, string) {
	t.Helper()
	registry := fixtureregistry.NewPluginRegistry(t)
	acquisition := registryAcquisition(registry)
	registry.SetAliases("community-check")
	module, err := os.ReadFile("../../../../plugins/execution/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, acquisition)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), "community-check", "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# keep user settings\nplugins:\n  - name: community-check\n    version: latest\n    registry: "+fixtureregistry.FixtureRegistryURL+"\nlint: {rules: {preset: none}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(path), plugininstall.LockName)
	lock, err := plugininstall.OpenLock(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := plugininstall.Sync(t.Context(), lock.Lock, acquisition); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	return registry, path, lockPath
}

func TestLegacyLockMigrationFreezesRecordedRelease(t *testing.T) {
	t.Parallel()
	registry, path, lockPath := legacyRegistryProject(t)
	acquisition := registryAcquisition(registry)
	registry.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := plugininstall.PrepareConfigurationMigration(t.Context(), path, plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Changed || !bytes.Contains(plan.Content, []byte("v1.0.0")) || bytes.Contains(plan.Content, []byte("latest")) {
		t.Fatalf("preview lost the locked release: %s", plan.Content)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview rewrote config")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatal("preview removed legacy lock")
	}
	registry.SetOffline(true)
	if err := plan.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg, err := checking.ConfigurationRules().Load(path)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != fixtureregistry.FixturePluginName || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("migration advanced or lost identity: %+v, %v", cfg, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("legacy lock retained: %v", err)
	}
	if !bytes.Contains(plan.Content, []byte("v1.0.0")) {
		t.Fatal("migration report differs from saved declarations")
	}
	if _, err := (plugininstall.Selection{ConfigFile: path, RuleSet: checking.ConfigurationRules(), Acquisition: acquisition}).ResolveForRules(t.Context(), filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	second, err := plugininstall.PrepareConfigurationMigration(t.Context(), path, plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition})
	if err != nil || second.Changed {
		t.Fatalf("migration is not stable: %+v, %v", second, err)
	}
}

func TestLegacyLockMigrationRejectsConflictingRegistryBinding(t *testing.T) {
	t.Parallel()
	registry, path, lockPath := legacyRegistryProject(t)
	acquisition := registryAcquisition(registry)
	lock, err := plugininstall.OpenLock(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock.Lock.Plugins[0].SHA256 = strings.Repeat("0", 64)
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := plugininstall.PrepareConfigurationMigration(t.Context(), path, plugininstall.DefaultPluginOptions{RuleSet: checking.ConfigurationRules(), Acquisition: acquisition})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(t.Context()); err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Fatalf("conflicting pin adopted: %v", err)
	}
	afterConfig, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(beforeConfig, afterConfig) {
		t.Fatal("failed migration changed config")
	}
	afterLock, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(beforeLock, afterLock) {
		t.Fatal("failed migration changed legacy lock")
	}
}
