package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestPluginMutationsWithoutDependenciesPreserveProject(t *testing.T) {
	for _, content := range []string{"", "# keep configuration\nlint: {}\n", "plugins: []\n"} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("ACTIONLINT_CONFIG", "")
			t.Setenv("ACTIONLINT_NO_CONFIG", "")
			if err := os.Mkdir(".git", 0o700); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(".github", "actionlint.yaml")
			if content != "" {
				if err := os.Mkdir(".github", 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, operation := range []string{"update", "install"} {
				output := pluginCommandTest(t, plugininstall.Acquisition{}, 0, "plugin", operation)
				if !strings.Contains(output, "No plugins installed.") {
					t.Fatal(output)
				}
			}
			if content == "" {
				if _, err := os.Stat(".github"); !os.IsNotExist(err) {
					t.Fatalf("empty plugin management created project files: %v", err)
				}
				return
			}
			got, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(got, []byte(content)) {
				t.Fatalf("empty plugin management changed configuration: %q, %v", got, err)
			}
			entries, err := os.ReadDir(".github")
			if err != nil || len(entries) != 1 || entries[0].Name() != "actionlint.yaml" {
				t.Fatalf("empty plugin management created lock or guard files: %v, %v", entries, err)
			}
		})
	}
}

func TestPluginInstallMissingExplicitPathsPreservesProject(t *testing.T) {
	for _, flag := range []string{"--config"} {
		t.Run(flag, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("ACTIONLINT_CONFIG", "")
			t.Setenv("ACTIONLINT_NO_CONFIG", "")
			pluginCommandTest(t, plugininstall.Acquisition{}, 0, "plugin", "install", flag, filepath.Join("missing", "project.yaml"))
			entries, err := os.ReadDir(".")
			if err != nil || len(entries) != 0 {
				t.Fatalf("empty install created files: %v, %v", entries, err)
			}
		})
	}
}

func TestPluginManagementIgnoresUnrelatedProjectJSON(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	for _, directory := range []string{".git", ".github"} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(".github", "actionlint.plugins.lock.json")
	content := []byte("this file is not configuration\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"install", "update", "list", "sync"} {
		pluginCommandTest(t, plugininstall.Acquisition{}, 0, "plugin", operation)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatalf("plugin management changed an unrelated file: %q, %v", after, err)
	}
}
