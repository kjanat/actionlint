package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/pluginregistry"
	"actionlint.kjanat.dev/internal/testutil"
)

func TestRegistryInstallExplicitRegistryOverride(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	const mirror = "https://mirror.example/"
	for url, data := range r.Files {
		if relative, ok := strings.CutPrefix(url, testutil.FixtureRegistryURL); ok {
			r.Files[mirror+relative] = bytes.Clone(data)
		}
	}
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", testutil.FixtureRegistryURL)
	pluginCommandTest(t, 0, "plugin", "install", "publisher/plugin", "--registry", mirror)
	configPath := filepath.Join(".github", "actionlint.yaml")
	lockPath := filepath.Join(".github", pluginregistry.LockName)
	cfg, err := repoconfig.Load(configPath)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Registry != mirror {
		t.Fatalf("registry declaration: %#v, %v", cfg, err)
	}
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil || len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0].Registry != mirror {
		t.Fatalf("registry pin: %#v, %v", lock, err)
	}
	r.Offline = true
	requests := len(r.Requests)
	pluginCommandTest(t, 0, "plugin", "install")
	if len(r.Requests) != requests {
		t.Fatal("repeated installation contacted network")
	}
	r.Offline = false
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	record := r.RecordURL("v1.0.0")
	r.Files[record] = bytes.ReplaceAll(r.Files[record], []byte(`"owner_id":5`), []byte(`"owner_id":6`))
	pluginCommandTest(t, 3, "plugin", "install", "publisher/plugin", "--registry", testutil.FixtureRegistryURL)
	after, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected publisher changed lock: %v", err)
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatalf("rejected publisher changed declaration: %v", err)
	}
}

func TestRegistryDiscoveryRejectsMissingHostname(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	for _, registry := range []string{"https://:443/", "https://[]/", "https:///catalogue/"} {
		for _, args := range [][]string{{"plugin", "search"}, {"plugin", "versions", "publisher/plugin"}} {
			pluginCommandTest(t, 2, append(args, "--registry", registry)...)
		}
	}
	if len(r.Requests) != 0 {
		t.Fatalf("invalid endpoint reached transport: %v", r.Requests)
	}
}
