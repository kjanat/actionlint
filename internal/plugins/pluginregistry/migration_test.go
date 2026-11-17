package pluginregistry_test

import (
	"bytes"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixtureregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginregistry"
)

func TestRegistryTransportMigration(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.AddArtifact(t, "v1.0.0", "plugin.wasm", []byte{0, 97, 115, 109, 1, 0, 0, 0})
	registry.AddArtifact(t, "v2.0.0", "plugin.component.wasm", emptyComponent)
	registry.Publish(t, "v1.0.0", "v2.0.0")
	client := registryClient(t, registry)
	old, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	next, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if old.Kind != artifacts.Core || old.Asset != "plugin.wasm" || next.Kind != artifacts.Component || next.Asset != "plugin.component.wasm" {
		t.Fatalf("release selection lost: old=%+v next=%+v", old, next)
	}
	file := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{old}}
	options := artifacts.Options{CacheDir: t.TempDir(), Connections: registry.Connections()}
	statuses := pluginregistry.StatusOptions{Acquisition: fixtureAcquisition(registry)}
	if err := file.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	if err := file.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if file.Plugins[0] != old || len(registry.Requests()) != requests {
		t.Fatal("sync upgraded or resolved the pinned core release")
	}
	registry.SetOffline(false)
	if err := file.CheckReplacement(next, ""); err != nil {
		t.Fatalf("new version transport migration rejected: %v", err)
	}
	file.Plugins = []pluginregistry.Pin{next}
	if err := file.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryVersionedIdentityStillChecksReleaseEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, before, after string }{
		{"receipt asset", `"asset":"plugin.component.wasm"`, `"asset":"different.wasm"`},
		{"artifact kind", `"kind":"component"`, `"kind":"core"`},
		{"owner", `"owner_id":5`, `"owner_id":6`},
		{"metadata identity", `"id":"no-sudo"`, `"id":"other"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			registry := fixtureregistry.NewPluginRegistry(t)
			registry.AddArtifact(t, "v2.0.0", "plugin.component.wasm", emptyComponent)
			registry.Publish(t, "v2.0.0")
			url := registry.RecordURL("v2.0.0")
			registry.SetFile(url, bytes.Replace(registry.File(url), []byte(test.before), []byte(test.after), 1))
			if _, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest"); err == nil {
				t.Fatal("accepted inconsistent migration evidence")
			}
		})
	}
}

func TestReleaseReplacementPreservesIdentityAndImmutability(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	old, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*pluginregistry.Pin)
	}{
		{"same-version kind", func(p *pluginregistry.Pin) { p.Kind = artifacts.Core }},
		{"same-version asset", func(p *pluginregistry.Pin) {
			p.Asset = "new.wasm"
			p.URL = strings.Replace(p.URL, "plugin.wasm", p.Asset, 1)
		}},
		{"same-version interface", func(p *pluginregistry.Pin) { p.Contract = "wasm-core-json/1" }},
		{"new-version publisher", func(p *pluginregistry.Pin) {
			p.Version = "v2.0.0"
			p.URL = strings.Replace(p.URL, "v1.0.0", p.Version, 1)
			p.OwnerID = "6"
		}},
		{"new-version plugin ID", func(p *pluginregistry.Pin) {
			p.Version = "v2.0.0"
			p.URL = strings.Replace(p.URL, "v1.0.0", p.Version, 1)
			p.PluginID = "different"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := pluginregistry.ResolvedPlugins{Plugins: []pluginregistry.Pin{old}}
			next := old
			test.edit(&next)
			if err := file.CheckReplacement(next, ""); err == nil {
				t.Fatal("accepted forbidden replacement")
			}
			if file.Plugins[0] != old {
				t.Fatal("rejected replacement changed existing pin")
			}
		})
	}
}
