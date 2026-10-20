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

func TestLockResolvedRegistryOverride(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	original, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*pluginregistry.Pin, *pluginref.Reference)
	}{
		{"selected registry", nil},
		{"unselected registry", func(_ *pluginregistry.Pin, ref *pluginref.Reference) { ref.Registry = original.Registry }},
		{"unselected name", func(_ *pluginregistry.Pin, ref *pluginref.Reference) { ref.Name = "github.com/other/plugin" }},
		{"unselected version", func(_ *pluginregistry.Pin, ref *pluginref.Reference) { ref.Version = "v1.0.0" }},
		{"invalid declaration", func(_ *pluginregistry.Pin, ref *pluginref.Reference) { ref.Registry = "https://:443/" }},
		{"repository", func(pin *pluginregistry.Pin, _ *pluginref.Reference) { pin.RepositoryID = "20" }},
		{"owner", func(pin *pluginregistry.Pin, _ *pluginref.Reference) { pin.OwnerID = "15" }},
		{"plugin", func(pin *pluginregistry.Pin, _ *pluginref.Reference) { pin.PluginID = "other-plugin" }},
		{"kind", func(pin *pluginregistry.Pin, _ *pluginref.Reference) { pin.Kind = artifact.Core }},
		{"asset", func(pin *pluginregistry.Pin, _ *pluginref.Reference) {
			pin.Asset = "other.wasm"
			pin.URL = strings.Replace(pin.URL, "plugin.wasm", pin.Asset, 1)
		}},
		{"immutable digest", func(pin *pluginregistry.Pin, _ *pluginref.Reference) { pin.SHA256 = strings.Repeat("0", 64) }},
		{"immutable interface", func(pin *pluginregistry.Pin, _ *pluginref.Reference) { pin.Contract = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := pluginregistry.LockFile{Lock: pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{original}}}
			pin := original
			pin.Registry = "https://mirror.example/"
			ref := pluginref.Reference{Name: pin.Name, Version: pin.Selector, Registry: pin.Registry}
			if test.change != nil {
				test.change(&pin, &ref)
			}
			err := file.SetResolved(pin, ref)
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
