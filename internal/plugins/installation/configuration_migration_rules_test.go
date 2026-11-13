package installation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/repositoryfixture"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestCategoryMigrationUsesCachedManifestWithoutExecuting(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("HOME", cache)
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	manifest := manifests.Manifest{
		SchemaVersion: 1, ID: "migration-fixture", Version: "1.0.0", Role: manifests.RoleDocument,
		Scope: manifests.ScopeSource, Transport: manifests.TransportProcess, Preparation: "generic",
		Rule:      rule.Descriptor{Name: "migration-fixture", Description: "Cached migration fixture", Category: "security", Subcategory: "workflow", Maturity: "stable", Provider: "migration-fixture", DocumentationURL: "https://example.invalid/migration-fixture"},
		Selectors: manifests.Selectors{Documents: []string{"workflow"}},
		Process:   &manifests.ProcessSpec{Entry: "must-not-execute-during-migration", Args: []string{}, Stdin: manifests.StdinRequest, Config: manifests.ConfigRendering{Render: manifests.RenderNone}, Output: manifests.Output{Format: manifests.FormatEnvelope}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := (artifacts.Store{Dir: DefaultArtifactOptions().CacheDir}).Publish(t.Context(), digest, data); err != nil {
		t.Fatal(err)
	}
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, "actionlint.yaml")
	original := []byte("plugins: ['https://plugins.example.invalid/custom.json#sha256=" + digest + "']\nlint: {rules: {plugins: {" + manifest.Rule.Name + ": warn}}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	migration, err := PrepareConfigurationMigration(t.Context(), path, DefaultPluginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(migration.Content, []byte("security:")) || !bytes.Contains(migration.Content, []byte(manifest.Rule.Name+": warn")) {
		t.Fatalf("plugin selection not moved: %s", migration.Content)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("preview changed the source")
	}
	if err := migration.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(current, migration.Content) {
		t.Fatal("migration did not write the checked candidate")
	}
}

func TestCheckingDefersUninspectableLegacyPluginSettings(t *testing.T) {
	t.Parallel()
	root := repositoryfixture.Directory(t)
	path := filepath.Join(root, "actionlint.yaml")
	original := []byte("plugins: []\nlint: {rules: {plugins: {custom-check: off}}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateConfiguration(t.Context(), path, DefaultPluginOptions{}); err != nil {
		t.Fatalf("ordinary checking forced plugin metadata migration: %v", err)
	}
	if _, err := PrepareConfigurationMigration(t.Context(), path, DefaultPluginOptions{}); err == nil || !strings.Contains(err.Error(), config.ErrRuleMetadataRequired.Error()) {
		t.Fatalf("explicit migration guessed plugin category: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("unavailable plugin metadata changed config")
	}
}
