package localapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

func TestCategoryMigrationUsesCachedManifestWithoutExecuting(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	manifest := nativeDocumentManifest(t)
	manifest.Rule.Category = "security"
	manifest.Process.Entry = "must-not-execute-during-migration"
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := (artifact.Store{Dir: artifactOptions().CacheDir}).Publish(t.Context(), digest, data); err != nil {
		t.Fatal(err)
	}
	root := pluginRepository(t)
	path := filepath.Join(root, "actionlint.yaml")
	original := []byte("plugins: ['https://plugins.example.invalid/custom.json#sha256=" + digest + "']\nlint: {rules: {plugins: {" + manifest.Rule.Name + ": warn}}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	migration, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{})
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
	root := pluginRepository(t)
	path := filepath.Join(root, "actionlint.yaml")
	original := []byte("plugins: []\nlint: {rules: {plugins: {custom-check: off}}}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateConfig(t.Context(), path, DefaultPluginOptions{}); err != nil {
		t.Fatalf("ordinary checking forced plugin metadata migration: %v", err)
	}
	if _, err := PrepareConfigMigration(t.Context(), path, DefaultPluginOptions{}); err == nil || !strings.Contains(err.Error(), config.ErrRuleMetadataRequired.Error()) {
		t.Fatalf("explicit migration guessed plugin category: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("unavailable plugin metadata changed config")
	}
}
