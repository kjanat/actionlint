package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	plugincmd "actionlint.kjanat.dev/v2/internal/cli/plugin"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func TestRegistryInstallExplicitRegistryOverride(t *testing.T) {
	r := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: r.UserCacheDir(), Connections: r.Connections()}
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	r.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	const mirror = "https://mirror.example/"
	for url, data := range r.Files() {
		if relative, ok := strings.CutPrefix(url, fixtureregistry.FixtureRegistryURL); ok {
			r.SetFile(mirror+relative, data)
		}
	}
	t.Setenv("ACTIONLINT_CONFIG", "")
	t.Setenv("ACTIONLINT_NO_CONFIG", "")
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".git", 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(".github", "actionlint.yaml")
	pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin@v1.0.0", "--registry", fixtureregistry.FixtureRegistryURL)
	installed := pluginCommandTest(t, acquisition, 0, "plugin", "install", "publisher/plugin", "--registry", mirror, "--json")
	var selected plugininstall.Lock
	if err := json.Unmarshal([]byte(installed), &selected); err != nil || len(selected.Plugins) != 1 {
		t.Fatalf("invalid installed lock selection: %s: %v", installed, err)
	}
	cfg, err := checking.ConfigurationRules().Load(configPath)
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Registry() != mirror || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("registry declaration: %#v, %v", cfg, err)
	}
	if selected.Plugins[0].Registry != mirror || selected.Plugins[0].Selector != "v1.0.0" || selected.Plugins[0].Version != "v1.0.0" {
		t.Fatalf("registry selection: %#v", selected.Plugins[0])
	}
	r.SetOffline(true)
	requests := len(r.Requests())
	pluginCommandTest(t, acquisition, 0, "plugin", "install")
	if len(r.Requests()) != requests {
		t.Fatal("repeated installation contacted network")
	}
	r.SetOffline(false)
	lockPath := filepath.Join(".github", plugininstall.LockName)
	legacyLock := bytes.ReplaceAll([]byte(installed), []byte(`"owner_id":"5"`), []byte(`"owner_id":"6"`))
	if bytes.Equal(legacyLock, []byte(installed)) {
		t.Fatal("installed selection has no publisher identity to vary")
	}
	if err := os.WriteFile(lockPath, legacyLock, 0o600); err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	pluginCommandTest(t, acquisition, 3, "plugin", "install", "publisher/plugin", "--registry", fixtureregistry.FixtureRegistryURL, "--plugin-lock", lockPath)
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(legacyLock, lockAfter) {
		t.Fatalf("rejected publisher changed legacy lock: %v", err)
	}
	configAfter, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatalf("rejected publisher changed declaration: %v", err)
	}
}

func TestRegistryInstallDeclarationSelection(t *testing.T) {
	t.Parallel()
	const first = "https://first.example/"
	const second = "https://second.example/"
	const third = "https://third.example/"
	for _, test := range []struct {
		name, registry string
		count, index   int
		version        string
		invalid        bool
	}{
		{"publisher/plugin", third, 1, 0, "v1", false},
		{"publisher/plugin", "", 2, -1, "", true},
		{"publisher/plugin", third, 2, -1, "", true},
		{"publisher/plugin", second, 2, 1, "v2", false},
		{"publisher/plugin", "https://second.example:443/", 2, 1, "v2", false},
		{"check", third, 1, -1, "latest", false},
		{"check", third, 2, -1, "latest", false},
		{"check", second, 2, 1, "v2", false},
		{"check", "https://second.example:443/", 2, 1, "v2", false},
		{"check", "", 2, -1, "", true},
	} {
		t.Run(test.name+test.registry, func(t *testing.T) {
			t.Parallel()
			one, err := pluginref.New(test.name, "v1", first)
			if err != nil {
				t.Fatal(err)
			}
			two := testReference(t, test.name, "v2", second)
			refs := []pluginref.Reference{one, two}[:test.count]
			ref, err := plugincmd.RequestedPlugin(test.name, plugincmd.RegistryOptions{Registry: test.registry}, refs)
			if test.invalid {
				if err == nil {
					t.Fatal("ambiguous declaration selected")
				}
				return
			}
			if err != nil || ref.Version() != test.version || ref.Registry() != test.registry {
				t.Fatalf("selection: %+v, %v", ref, err)
			}
			index, err := plugincmd.RequestedPluginIndex(ref, test.registry, refs)
			if err != nil || index != test.index {
				t.Fatalf("declaration index=%d, error=%v; want %d", index, err, test.index)
			}
		})
	}
}

func TestRegistryDiscoveryRejectsMissingHostname(t *testing.T) {
	r := fixtureregistry.NewPluginRegistry(t)
	acquisition := plugininstall.Acquisition{UserCacheDir: r.UserCacheDir(), Connections: r.Connections()}
	t.Chdir(t.TempDir())
	for _, registry := range []string{"https://:443/", "https://[]/", "https:///catalogue/"} {
		for _, args := range [][]string{{"plugin", "search"}, {"plugin", "versions", "publisher/plugin"}} {
			pluginCommandTest(t, acquisition, 2, append(args, "--registry", registry)...)
		}
	}
	if len(r.Requests()) != 0 {
		t.Fatalf("invalid endpoint reached transport: %v", r.Requests())
	}
}

func TestRegistryInstallDirectURLDeclarationSelection(t *testing.T) {
	t.Parallel()
	const source = "https://plugins.example/check.wasm#sha256="
	old, err := pluginref.Parse(source + strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	updated := source + strings.Repeat("2", 64)
	ref, err := plugincmd.RequestedPlugin(updated, plugincmd.RegistryOptions{}, []pluginref.Reference{old})
	if err != nil || ref.DirectSource() != updated {
		t.Fatalf("direct selection: %+v, %v", ref, err)
	}
	index, err := plugincmd.RequestedPluginIndex(ref, "", []pluginref.Reference{old})
	if err != nil || index != 0 {
		t.Fatalf("direct replacement index=%d, error=%v", index, err)
	}
}
