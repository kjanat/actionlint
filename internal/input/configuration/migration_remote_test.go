package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/input/files"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

func TestMigrationRemoteDependencySnapshot(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	path := filepath.Join(files.PhysicalPath(t.TempDir()), "actionlint.yaml")
	remote := fixtureregistry.FixtureRegistryURL + "base.yaml"
	original := "extends: [" + remote + "]\nconfig-variables: [LOCAL]\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	body := []byte("policy: {require-commit-hash: true}")
	registry.SetFile(remote, body)
	plan, err := checking.ConfigurationRules().PrepareMigration(t.Context(), path, registry.Connections())
	if err != nil {
		t.Fatal(err)
	}
	if requests := registry.Requests(); len(requests) != 1 || requests[0] != remote {
		t.Fatalf("preparation requests = %v", requests)
	}
	if err := plan.CheckSources(map[string][]byte{path: []byte(original), remote: body}); err != nil {
		t.Fatalf("matching captured graph: %v", err)
	}
	if err := plan.CheckSources(map[string][]byte{path: []byte(original), remote: []byte("plugins: [different/plugin]")}); err == nil || !strings.Contains(err.Error(), remote) {
		t.Fatalf("mismatched remote snapshot = %v", err)
	}
	if err := plan.CheckSources(map[string][]byte{path: []byte(original)}); err == nil {
		t.Fatal("accepted an incomplete captured graph")
	}
	if requests := registry.Requests(); len(requests) != 1 {
		t.Fatalf("comparing captured graphs performed network IO: %v", requests)
	}
	if err := plan.CheckUnchanged(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests := registry.Requests(); len(requests) != 2 || requests[1] != remote {
		t.Fatalf("source check requests = %v", requests)
	}
	registry.SetFile(remote, []byte("policy: {require-commit-hash: false}"))
	registry.ResetRequests()
	if err := plan.Apply(t.Context()); err == nil || !strings.Contains(err.Error(), "changed before migration") || !strings.Contains(err.Error(), remote) {
		t.Fatalf("changed remote error = %v", err)
	}
	if requests := registry.Requests(); len(requests) != 1 || requests[0] != remote {
		t.Fatalf("changed-source check requests = %v", requests)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("leaf after rejection = %q, %v", content, err)
	}
}

func TestMigrationRemoteLeafReadOnly(t *testing.T) {
	t.Parallel()
	_, err := checking.ConfigurationRules().PrepareMigration(t.Context(), "https://example.test/actionlint.yaml", acquisitionhttp.Connections{})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("remote leaf error = %v", err)
	}
}
