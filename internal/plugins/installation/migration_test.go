package installation_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
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
	file, err := plugininstall.OpenLock(t.Context(), filepath.Join(t.TempDir(), plugininstall.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Set(old); err != nil {
		t.Fatal(err)
	}
	options := artifacts.Options{CacheDir: t.TempDir(), Connections: registry.Connections()}
	statuses := plugininstall.StatusOptions{Acquisition: fixtureAcquisition(registry)}
	if err := file.Lock.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	registry.SetOffline(true)
	requests := len(registry.Requests())
	if err := file.Lock.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if file.Lock.Plugins[0] != old || len(registry.Requests()) != requests {
		t.Fatal("sync upgraded or resolved the pinned core release")
	}
	registry.SetOffline(false)
	if err := file.Set(next); err != nil {
		t.Fatalf("new version transport migration rejected: %v", err)
	}
	if err := file.Lock.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if err := file.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := plugininstall.OpenLock(t.Context(), file.Path)
	if err != nil || len(reopened.Lock.Plugins) != 1 || reopened.Lock.Plugins[0] != next {
		t.Fatalf("migrated pin not preserved: %v %v", reopened, err)
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

func TestLockMigrationPreservesIdentityAndImmutability(t *testing.T) {
	t.Parallel()
	registry := fixtureregistry.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	old, err := registryClient(t, registry).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*plugininstall.Pin)
	}{
		{"same-version kind", func(p *plugininstall.Pin) { p.Kind = artifacts.Core }},
		{"same-version asset", func(p *plugininstall.Pin) {
			p.Asset = "new.wasm"
			p.URL = strings.Replace(p.URL, "plugin.wasm", p.Asset, 1)
		}},
		{"same-version interface", func(p *plugininstall.Pin) { p.Contract = "wasm-core-json/1" }},
		{"new-version publisher", func(p *plugininstall.Pin) {
			p.Version = "v2.0.0"
			p.URL = strings.Replace(p.URL, "v1.0.0", p.Version, 1)
			p.OwnerID = "6"
		}},
		{"new-version plugin ID", func(p *plugininstall.Pin) {
			p.Version = "v2.0.0"
			p.URL = strings.Replace(p.URL, "v1.0.0", p.Version, 1)
			p.PluginID = "different"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := plugininstall.LockFile{Lock: plugininstall.Lock{SchemaVersion: 1, Plugins: []plugininstall.Pin{old}}}
			next := old
			test.edit(&next)
			if err := file.Set(next); err == nil {
				t.Fatal("accepted forbidden replacement")
			}
			if file.Lock.Plugins[0] != old {
				t.Fatal("failed migration changed existing pin")
			}
		})
	}
}
