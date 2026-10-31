package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRegistryInstallExplicitRegistryOverride(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	r.Add(t, "v2.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
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
	if err != nil || len(cfg.Plugins) != 1 || cfg.Plugins[0].Registry() != mirror || cfg.Plugins[0].Version() != "v1.0.0" {
		t.Fatalf("registry declaration: %#v, %v", cfg, err)
	}
	lock, err := pluginregistry.OpenLock(t.Context(), lockPath)
	if err != nil || len(lock.Lock.Plugins) != 1 || lock.Lock.Plugins[0].Registry != mirror || lock.Lock.Plugins[0].Selector != "v1.0.0" || lock.Lock.Plugins[0].Version != "v1.0.0" {
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

func TestRegistryInstallDeclarationSelection(t *testing.T) {
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
			one, err := pluginref.New(test.name, "v1", first)
			if err != nil {
				t.Fatal(err)
			}
			two := testReference(t, test.name, "v2", second)
			refs := []pluginref.Reference{one, two}[:test.count]
			ref, err := requestedPlugin(test.name, pluginInvocation{Registry: test.registry}, refs)
			if test.invalid {
				if err == nil {
					t.Fatal("ambiguous declaration selected")
				}
				return
			}
			if err != nil || ref.Version() != test.version || ref.Registry() != test.registry {
				t.Fatalf("selection: %+v, %v", ref, err)
			}
			index, err := requestedPluginIndex(ref, test.registry, refs)
			if err != nil || index != test.index {
				t.Fatalf("declaration index=%d, error=%v; want %d", index, err, test.index)
			}
		})
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

func TestRegistryInstallDirectURLDeclarationSelection(t *testing.T) {
	const source = "https://plugins.example/check.wasm#sha256="
	old, err := pluginref.Parse(source + strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	updated := source + strings.Repeat("2", 64)
	ref, err := requestedPlugin(updated, pluginInvocation{}, []pluginref.Reference{old})
	if err != nil || ref.DirectSource() != updated {
		t.Fatalf("direct selection: %+v, %v", ref, err)
	}
	index, err := requestedPluginIndex(ref, "", []pluginref.Reference{old})
	if err != nil || index != 0 {
		t.Fatalf("direct replacement index=%d, error=%v", index, err)
	}
}
