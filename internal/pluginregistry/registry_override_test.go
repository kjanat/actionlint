package pluginregistry_test

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

// declaration holds the inputs to pluginref.New so a test case can change one
// of them before the Reference is built.
type declaration struct{ name, version, registry string }

func TestLockResolvedRegistryOverride(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	original, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*pluginregistry.Pin, *declaration)
	}{
		{"selected registry", nil},
		{"unselected registry", func(_ *pluginregistry.Pin, ref *declaration) { ref.registry = original.Registry }},
		{"unselected name", func(_ *pluginregistry.Pin, ref *declaration) { ref.name = "github.com/other/plugin" }},
		{"unselected version", func(_ *pluginregistry.Pin, ref *declaration) { ref.version = "v1.0.0" }},
		{"repository", func(pin *pluginregistry.Pin, _ *declaration) { pin.RepositoryID = "20" }},
		{"owner", func(pin *pluginregistry.Pin, _ *declaration) { pin.OwnerID = "15" }},
		{"plugin", func(pin *pluginregistry.Pin, _ *declaration) { pin.PluginID = "other-plugin" }},
		{"kind", func(pin *pluginregistry.Pin, _ *declaration) { pin.Kind = artifact.Core }},
		{"asset", func(pin *pluginregistry.Pin, _ *declaration) {
			pin.Asset = "other.wasm"
			pin.URL = strings.Replace(pin.URL, "plugin.wasm", pin.Asset, 1)
		}},
		{"immutable digest", func(pin *pluginregistry.Pin, _ *declaration) { pin.SHA256 = strings.Repeat("0", 64) }},
		{"immutable interface", func(pin *pluginregistry.Pin, _ *declaration) { pin.Contract = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := pluginregistry.LockFile{Lock: pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{original}}}
			pin := original
			pin.Registry = "https://mirror.example/"
			declared := declaration{name: pin.Name, version: pin.Selector, registry: pin.Registry}
			if test.change != nil {
				test.change(&pin, &declared)
			}
			ref, err := pluginref.New(declared.name, declared.version, declared.registry)
			if err != nil {
				t.Fatal(err)
			}
			err = file.SetResolved(pin, ref)
			if test.change == nil {
				if err != nil || !reflect.DeepEqual(file.Lock.Plugins, []pluginregistry.Pin{pin}) {
					t.Fatalf("selected mirror: %#v, %v", file.Lock, err)
				}
			} else if err == nil || !reflect.DeepEqual(file.Lock.Plugins, []pluginregistry.Pin{original}) {
				t.Fatalf("rejected replacement changed lock: %#v, %v", file.Lock, err)
			}
		})
	}
}

func TestRegistryURLRequiresHostname(t *testing.T) {
	for _, registry := range []string{"https://:443/", "https://[]/", "https:///catalogue/"} {
		if _, err := pluginregistry.New(registry); err == nil {
			t.Errorf("accepted hostname-less endpoint %q", registry)
		}
	}
}
