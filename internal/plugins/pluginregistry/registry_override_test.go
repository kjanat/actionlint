package pluginregistry_test

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

type declaration struct{ name, version, registry string }

func TestResolvedRegistryOverride(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", analyzerComponent)
	original, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
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
		{"kind", func(pin *pluginregistry.Pin, _ *declaration) { pin.Kind = artifacts.Core }},
		{"asset", func(pin *pluginregistry.Pin, _ *declaration) {
			pin.Asset = "other.wasm"
			pin.URL = strings.Replace(pin.URL, "plugin.wasm", pin.Asset, 1)
		}},
		{"immutable digest", func(pin *pluginregistry.Pin, _ *declaration) { pin.SHA256 = strings.Repeat("0", 64) }},
		{"immutable interface", func(pin *pluginregistry.Pin, _ *declaration) { pin.Contract = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{original}}
			pin := original
			pin.Registry = "https://mirror.example/"
			declared := declaration{name: pin.Name, version: pin.Selector, registry: pin.Registry}
			if test.change != nil {
				test.change(&pin, &declared)
			}
			ref, err := pluginreference.New(declared.name, declared.version, declared.registry)
			if err != nil {
				t.Fatal(err)
			}
			err = file.CheckResolved(pin, ref)
			if test.change == nil {
				if err != nil {
					t.Fatalf("selected mirror: %#v, %v", file, err)
				}
			} else if err == nil || !reflect.DeepEqual(file.Plugins, []pluginregistry.Pin{original}) {
				t.Fatalf("rejected replacement changed releases: %#v, %v", file, err)
			}
		})
	}
}

func TestRegistryURLRequiresHostname(t *testing.T) {
	t.Parallel()
	for _, registry := range []string{"https://:443/", "https://[]/", "https:///catalogue/"} {
		if _, err := pluginregistry.New(registry, dependencycache.Acquisition{}); err == nil {
			t.Errorf("accepted hostname-less endpoint %q", registry)
		}
	}
}

func TestRegistryURLRequiresDialableAuthority(t *testing.T) {
	t.Parallel()
	for _, registry := range []string{"https://under_score.example/", "https://catalogue.example:65536/"} {
		if _, err := pluginregistry.New(registry, dependencycache.Acquisition{}); err == nil {
			t.Errorf("accepted undialable endpoint %q", registry)
		}
		if pluginregistry.SameRegistry(registry, registry) {
			t.Errorf("SameRegistry accepted undialable endpoint %q", registry)
		}
	}
}
