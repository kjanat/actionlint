package pluginregistry_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRegistryTransportMigration(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.AddArtifact(t, "v1.0.0", "plugin.wasm", []byte{0, 97, 115, 109, 1, 0, 0, 0})
	registry.AddArtifact(t, "v2.0.0", "plugin.component.wasm", emptyComponent)
	registry.Publish(t, "v1.0.0", "v2.0.0")
	client := registryClient(t)
	old, err := client.Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	next, err := client.Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if old.Kind != artifact.Core || old.Asset != "plugin.wasm" || next.Kind != artifact.Component || next.Asset != "plugin.component.wasm" {
		t.Fatalf("release selection lost: old=%+v next=%+v", old, next)
	}
	file, err := pluginregistry.OpenLock(t.Context(), filepath.Join(t.TempDir(), pluginregistry.LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Set(old); err != nil {
		t.Fatal(err)
	}
	options := artifact.Options{CacheDir: t.TempDir()}
	if err := file.Lock.Sync(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	registry.Offline = true
	requests := len(registry.Requests)
	if err := file.Lock.Sync(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	if file.Lock.Plugins[0] != old || len(registry.Requests) != requests {
		t.Fatal("sync upgraded or resolved the pinned core release")
	}
	registry.Offline = false
	if err := file.Set(next); err != nil {
		t.Fatalf("new version transport migration rejected: %v", err)
	}
	if err := file.Lock.Sync(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	if err := file.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := pluginregistry.OpenLock(t.Context(), file.Path)
	if err != nil || len(reopened.Lock.Plugins) != 1 || reopened.Lock.Plugins[0] != next {
		t.Fatalf("migrated pin not preserved: %v %v", reopened, err)
	}
}

func TestRegistryVersionedIdentityStillChecksReleaseEvidence(t *testing.T) {
	for _, test := range []struct{ name, before, after string }{
		{"receipt asset", `"asset":"plugin.component.wasm"`, `"asset":"different.wasm"`},
		{"artifact kind", `"kind":"component"`, `"kind":"core"`},
		{"owner", `"owner_id":5`, `"owner_id":6`},
		{"metadata identity", `"id":"no-sudo"`, `"id":"other"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := testutil.NewPluginRegistry(t)
			registry.AddArtifact(t, "v2.0.0", "plugin.component.wasm", emptyComponent)
			registry.Publish(t, "v2.0.0")
			url := registry.RecordURL("v2.0.0")
			registry.Files[url] = bytes.Replace(registry.Files[url], []byte(test.before), []byte(test.after), 1)
			if _, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil {
				t.Fatal("accepted inconsistent migration evidence")
			}
		})
	}
}

func TestLockMigrationPreservesIdentityAndImmutability(t *testing.T) {
	registry := testutil.NewPluginRegistry(t)
	registry.Add(t, "v1.0.0", emptyComponent)
	old, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*pluginregistry.Pin)
	}{
		{"same-version kind", func(p *pluginregistry.Pin) { p.Kind = artifact.Core }},
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
			file := pluginregistry.LockFile{Lock: pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{old}}}
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
