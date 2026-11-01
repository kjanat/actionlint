package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestExplicitRegistryGrantDoesNotPersistIntoConfiguration(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	registry.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	const private = "https://127.0.0.1:8443/catalogue/"
	for location, data := range registry.Files() {
		if relative, found := strings.CutPrefix(location, testutil.FixtureRegistryURL); found {
			registry.SetFile(private+relative, data)
		}
	}
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", private)
	path := filepath.Join(".github", "actionlint.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Select an uncached release through the saved declaration. The previous
	// invocation's endpoint grant must not be reconstructed from that file.
	updated := strings.Replace(string(content), "v1.0.0", "v2.0.0", 1)
	if updated == string(content) {
		t.Fatal("installed declaration has no exact version")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := len(registry.Requests())
	output := pluginCommandTest(t, 3, "plugin", "list")
	if !strings.Contains(output, "public destination") || len(registry.Requests()) != requests {
		t.Fatalf("configuration regained prior endpoint grant: %s", output)
	}
}
