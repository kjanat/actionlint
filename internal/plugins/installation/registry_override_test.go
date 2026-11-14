package installation_test

import (
	"reflect"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

// declaration holds the inputs to pluginref.New so a test case can change one
// of them before the Reference is built.
type declaration struct{ name, version, registry string }

func TestResolvedRegistryOverride(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	original, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*plugininstall.Pin, *declaration)
	}{
		{"selected registry", nil},
		{"unselected registry", func(_ *plugininstall.Pin, ref *declaration) { ref.registry = original.Registry }},
		{"unselected name", func(_ *plugininstall.Pin, ref *declaration) { ref.name = "github.com/other/plugin" }},
		{"unselected version", func(_ *plugininstall.Pin, ref *declaration) { ref.version = "v1.0.0" }},
		{"repository", func(pin *plugininstall.Pin, _ *declaration) { pin.RepositoryID = "20" }},
		{"owner", func(pin *plugininstall.Pin, _ *declaration) { pin.OwnerID = "15" }},
		{"plugin", func(pin *plugininstall.Pin, _ *declaration) { pin.PluginID = "other-plugin" }},
		{"kind", func(pin *plugininstall.Pin, _ *declaration) { pin.Kind = artifacts.Core }},
		{"asset", func(pin *plugininstall.Pin, _ *declaration) {
			pin.Asset = "other.wasm"
			pin.URL = strings.Replace(pin.URL, "plugin.wasm", pin.Asset, 1)
		}},
		{"immutable digest", func(pin *plugininstall.Pin, _ *declaration) { pin.SHA256 = strings.Repeat("0", 64) }},
		{"immutable interface", func(pin *plugininstall.Pin, _ *declaration) { pin.Contract = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{original}}
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
			err = file.CheckResolved(pin, ref)
			if test.change == nil {
				if err != nil {
					t.Fatalf("selected mirror: %#v, %v", file, err)
				}
			} else if err == nil || !reflect.DeepEqual(file.Plugins, []plugininstall.Pin{original}) {
				t.Fatalf("rejected replacement changed releases: %#v, %v", file, err)
			}
		})
	}
}

func TestRegistryURLRequiresHostname(t *testing.T) {
	t.Parallel()
	for _, registry := range []string{"https://:443/", "https://[]/", "https:///catalogue/"} {
		if _, err := plugininstall.New(registry, plugininstall.Acquisition{}); err == nil {
			t.Errorf("accepted hostname-less endpoint %q", registry)
		}
	}
}
