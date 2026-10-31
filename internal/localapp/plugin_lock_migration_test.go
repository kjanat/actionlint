package localapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func legacyRegistryProject(t *testing.T) (*testutil.PluginRegistry, string, string) {
	t.Helper()
	registry := testutil.NewPluginRegistry(t)
	registry.SetAliases("community-check")
	module, err := os.ReadFile("../plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.0.0", module)
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), "community-check", "latest")
	if err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# keep user settings\nplugins:\n  - name: community-check\n    version: latest\n    registry: "+testutil.FixtureRegistryURL+"\nlint: {rules: {preset: none}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(path), pluginregistry.LockName)
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := SyncPlugins(t.Context(), lock.Lock); err != nil {
		t.Fatal(err)
	}
	if err := lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	return registry, path, lockPath
}

func TestLegacyLockMigrationFreezesRecordedRelease(t *testing.T) {
	registry, path, lockPath := legacyRegistryProject(t)
	registry.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{})
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
	cfg, err := config.Load(path)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != testutil.FixturePluginName || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("migration advanced or lost identity: %+v, %v", cfg, err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("legacy lock retained: %v", err)
	}
	if !bytes.Contains(plan.Content, []byte("v1.0.0")) {
		t.Fatal("migration report differs from saved declarations")
	}
	if _, err := (PluginSelection{ConfigFile: path}).ResolveForRules(t.Context(), filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{})
	if err != nil || second.Changed {
		t.Fatalf("migration is not stable: %+v, %v", second, err)
	}
}

func TestLegacyLockMigrationRejectsConflictingRegistryBinding(t *testing.T) {
	_, path, lockPath := legacyRegistryProject(t)
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
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
	plan, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{})
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
