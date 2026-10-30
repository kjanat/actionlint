package localapp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestMigrationOverlayDisablesLockOnlyPlugins(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	path := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("config-variables: []\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := OpenPluginProject(t.Context(), root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := SyncPlugins(t.Context(), project.Lock.Lock); err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests)
	overlay, err := config.ParseOverlay("config", []byte("plugins: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := (PluginSelection{Overlays: []config.Overlay{overlay}}).Migrate(t.Context(), root, application.Input{Kind: application.Repository}, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}}); err != nil {
		t.Fatal(err)
	}
	migrated, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Plugins == nil || len(migrated.Plugins) != 0 {
		t.Fatalf("overlay opt-out lost to lock-only pins: %+v", migrated.Plugins)
	}
	if len(registry.Requests) != requests {
		t.Fatalf("overlay opt-out contacted registry: %v", registry.Requests[requests:])
	}
	if bytes.Equal(original, mustReadFile(t, path)) {
		t.Fatal("legacy config was not migrated")
	}
}

func TestAutomaticMigrationPreflightsAllRepositories(t *testing.T) {
	registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	registry.Offline = true
	first := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	second := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	firstConfig := filepath.Join(first, ".github", "actionlint.yaml")
	secondConfig := filepath.Join(second, ".github", "actionlint.yaml")
	input := application.Input{Kind: application.Files, Paths: []string{
		filepath.Join(first, ".github", "workflows", "first.yml"),
		filepath.Join(second, ".github", "workflows", "second.yml"),
	}}
	err := (PluginSelection{}).Migrate(t.Context(), first, input, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}})
	if err == nil || !strings.Contains(err.Error(), "inputs span repositories with plugin configuration") {
		t.Fatalf("migration error = %v, want multi-repository plugin error", err)
	}
	if len(registry.Requests) != 0 {
		t.Fatalf("migration resolved plugins before rejecting repository selection: %v", registry.Requests)
	}
	for _, path := range []string{firstConfig, secondConfig} {
		if got := mustReadFile(t, path); !bytes.Equal(got, []byte("config-variables: []\n")) {
			t.Errorf("preflight migrated %s: %q", path, got)
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(path), pluginregistry.LockName)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("preflight wrote lock beside %s: %v", path, err)
		}
	}
}

func TestAutomaticMigrationAllowsMultipleRepositoriesWithoutPlugins(t *testing.T) {
	registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	first := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	second := migrationRepositoryWithLegacyConfig(t, "config-secrets: []\n")
	input := application.Input{Kind: application.Files, Paths: []string{
		filepath.Join(first, ".github", "workflows", "first.yml"),
		filepath.Join(second, ".github", "workflows", "second.yml"),
	}}
	if err := (PluginSelection{}).Migrate(t.Context(), first, input, DefaultPluginOptions{}); err != nil {
		t.Fatal(err)
	}
	selected, err := (PluginSelection{}).Select(t.Context(), first, input)
	if err != nil {
		t.Fatalf("selecting multiple plugin-free repositories: %v", err)
	}
	if len(selected.Paths) != 0 || selected.LockFile != "" || len(registry.Requests) != 0 {
		t.Fatalf("plugin-free selection unexpectedly activated plugins: %+v, requests %v", selected, registry.Requests)
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
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	client, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	current := pluginRepository(t)
	project, err := OpenPluginProject(t.Context(), current, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Set(pin); err != nil {
		t.Fatal(err)
	}
	if err := SyncPlugins(t.Context(), project.Lock.Lock); err != nil {
		t.Fatal(err)
	}
	if err := project.Lock.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	refs, err := project.Dependencies(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), refs); err != nil {
		t.Fatal(err)
	}
	legacy := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	legacyPath := filepath.Join(legacy, ".github", "actionlint.yaml")
	input := application.Input{Kind: application.Files, Paths: []string{
		filepath.Join(current, ".github", "workflows", "current.yml"),
		filepath.Join(legacy, ".github", "workflows", "legacy.yml"),
	}}
	err = (PluginSelection{}).Migrate(t.Context(), current, input, DefaultPluginOptions{})
	if err == nil || !strings.Contains(err.Error(), "inputs span repositories with plugin configuration") {
		t.Fatalf("migration error = %v, want multi-repository plugin error", err)
	}
	if got := mustReadFile(t, legacyPath); !bytes.Equal(got, []byte("config-variables: []\n")) {
		t.Fatalf("migration changed legacy config beside active project: %q", got)
	}
}

func TestAutomaticMigrationRejectsPluginDefaultsWithUnmanagedInput(t *testing.T) {
	registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	registry.Offline = true
	root := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	legacyPath := filepath.Join(root, ".github", "actionlint.yaml")
	outside := t.TempDir()
	workflow := filepath.Join(outside, "workflow.yml")
	if err := os.WriteFile(workflow, []byte("on: push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := application.Input{Kind: application.Files, Paths: []string{
		filepath.Join(root, ".github", "workflows", "repo.yml"),
		workflow,
	}}
	err := (PluginSelection{}).Migrate(t.Context(), root, input, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}})
	if err == nil || !strings.Contains(err.Error(), "inputs span repositories with plugin configuration") {
		t.Fatalf("migration error = %v, want unmanaged-input plugin error", err)
	}
	if len(registry.Requests) != 0 {
		t.Fatalf("migration resolved plugins before rejecting unmanaged input: %v", registry.Requests)
	}
	if got := mustReadFile(t, legacyPath); !bytes.Equal(got, []byte("config-variables: []\n")) {
		t.Fatalf("migration changed config before rejecting unmanaged input: %q", got)
	}
}

func TestAutomaticMigrationDeduplicatesPhysicalRepositoryAliases(t *testing.T) {
	registry := testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	root := migrationRepositoryWithLegacyConfig(t, "config-variables: []\n")
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	input := application.Input{Kind: application.Files, Paths: []string{
		filepath.Join(root, ".github", "workflows", "root.yml"),
		filepath.Join(alias, ".github", "workflows", "alias.yml"),
	}}
	if err := (PluginSelection{}).Migrate(t.Context(), root, input, DefaultPluginOptions{Tools: []string{"shellcheck", "ruff"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".github", "actionlint.yaml")
	migrated, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrated.Plugins) != 2 {
		t.Fatalf("active defaults were not migrated through physical alias: %+v", migrated.Plugins)
	}
	if len(registry.Requests) == 0 {
		t.Fatal("active default plugins did not resolve")
	}
}

func migrationRepositoryWithLegacyConfig(t *testing.T, contents string) string {
	t.Helper()
	root := pluginRepository(t)
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
