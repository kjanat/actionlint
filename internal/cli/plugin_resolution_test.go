package cli

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

func TestRegistryCommandsUseExactConfigWithoutLock(t *testing.T) {
	module, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	registry := testutil.NewPluginRegistry(t)
	registry.SetAliases("community-check")
	registry.Add(t, "v1.0.0", module)
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.Mkdir(".git", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: sudo echo test}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "community-check", "--registry", testutil.FixtureRegistryURL)
	path := filepath.Join(".github", "actionlint.yaml")
	cfg, err := config.Load(path)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Name() != testutil.FixturePluginName || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("unresolved configuration: %+v, %v", cfg, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	registry.ResetRequests()
	registry.SetOffline(true)
	pluginCommandTest(t, 0, "plugin", "install")
	pluginCommandTest(t, 0, "plugin", "list")
	output := pluginCommandTest(t, 1, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	if !strings.Contains(output, "no-sudo") || len(registry.Requests()) != 0 {
		t.Fatalf("offline plugin did not execute: %s, %v", output, registry.Requests())
	}
	pluginCommandTest(t, 0, "plugin", "sync")
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("read-only commands rewrote config")
	}
	if _, err := os.Stat(filepath.Join(".github", pluginregistry.LockName)); !os.IsNotExist(err) {
		t.Fatalf("project lockfile created: %v", err)
	}
	registry.SetOffline(false)
	registry.Add(t, "v1.1.0", module)
	pluginCommandTest(t, 0, "plugin", "update", "community-check", "--version", "latest")
	cfg, err = config.Load(path)
	if err != nil || cfg.Plugins[0].Version() != "v1.1.0" {
		t.Fatalf("update did not persist an exact version: %+v, %v", cfg, err)
	}
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	registry.Add(t, "v1.2.0", module)
	registry.DeleteFile("https://" + testutil.FixturePluginName + "/releases/download/v1.2.0/plugin.wasm")
	pluginCommandTest(t, 3, "plugin", "update", testutil.FixturePluginName, "--version", "latest")
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed download changed configuration")
	}
	registry.Publish(t, "v1.0.0")
	pluginCommandTest(t, 3, "plugin", "update", testutil.FixturePluginName, "--version", "latest")
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("stale catalogue downgraded the configured version")
	}
}

func TestCheckingLegacyConfigDoesNotWriteProjectFiles(t *testing.T) {
	testutil.IsolateUserCache(t)
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	if err := os.MkdirAll(".github", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(".git", 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(".github", "actionlint.yaml")
	original := []byte("config-variables: [BUILD]\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("workflow.yml", []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "check", "--no-tool=ruff", "--no-tool=shellcheck", "workflow.yml")
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("checking migrated configuration on disk")
	}
	entries, err := os.ReadDir(".github")
	if err != nil || len(entries) != 1 {
		t.Fatalf("checking added project files: %v, %v", entries, err)
	}
}
