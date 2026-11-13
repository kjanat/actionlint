package installation

import (
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

func publishPackageFixture(t *testing.T, network *fixtureregistry.PluginRegistry, id, tag string, process bool) (CatalogPlugin, string) {
	t.Helper()
	data := network.AddPlugin(t, fixtureregistry.PluginPublisher{PluginID: id, BaseURL: fixtureregistry.FixtureRegistryURL, Name: fixtureregistry.FixturePluginName, RepositoryID: 10}, tag, []byte{0, 97, 115, 109, 13, 0, 1, 0})
	var catalog Catalog
	if err := jsonv2.Unmarshal(network.File(fixtureregistry.FixtureRegistryURL+"index.json"), &catalog); err != nil {
		t.Fatal(err)
	}
	p := catalog.Plugins[0]
	var m manifest
	if err := jsonv2.Unmarshal(network.File(fixtureregistry.FixtureRegistryURL+p.Manifest), &m); err != nil {
		t.Fatal(err)
	}
	var r record
	if err := jsonv2.Unmarshal(network.File(network.RecordURL(tag)), &r); err != nil {
		t.Fatal(err)
	}
	p.Name = strings.ToLower(id)
	p.Manifest = "plugins/" + hash("package-v2\n"+m.Plugin.Issuer+"\n"+m.Plugin.RepositoryID+"\n"+id) + "/index.json"
	m.Plugin.SchemaVersion = 2
	r.IdentityVersion = 2
	asset := strings.TrimPrefix(id, fixtureregistry.FixturePluginName+"/") + ".wasm"
	if process {
		asset = "process.json"
		metadata := fmt.Appendf(nil, `{"metadata_version":1,"transport":"process","id":%q,"plugin_version":%q,"repository":%q}`, id, strings.TrimPrefix(tag, "v"), "https://"+fixtureregistry.FixturePluginName)
		data = fmt.Appendf(nil, `{"schema_version":1,"transport":"process","metadata":%s}`, metadata)
		r.Artifact.Kind, r.Artifact.Contract, r.Artifact.Metadata = artifacts.ProviderManifest, "process/1", metadata
		r.Submission.Kind = artifacts.ProviderManifest
		r.Submission.SchemaVersion = 2
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	r.Artifact.SHA256 = digest
	r.Submission.Asset, r.Submission.SHA256 = asset, digest
	if process {
		r.Submission.Artifact = distribution{typeName: "release-asset", name: asset, digest: digest}
		r.Submission.Asset, r.Submission.SHA256 = "", ""
	}
	r.Receipt.Submission = r.Submission
	r.URL = "https://" + fixtureregistry.FixturePluginName + "/releases/download/" + tag + "/" + asset
	network.SetFile(r.URL, data)
	write := func(url string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		network.SetFile(url, data)
	}
	recordURL := fixtureregistry.FixtureRegistryURL + strings.TrimSuffix(p.Manifest, "index.json") + m.Versions[0].Record
	write(recordURL, r)
	write(fixtureregistry.FixtureRegistryURL+p.Manifest, m)
	return p, recordURL
}

func TestMonorepoPackagesResolveAndSyncIndependently(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	network := fixtureregistry.NewPluginRegistry(t)
	network.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	legacyFiles := network.Files()
	var catalog Catalog
	if err := jsonv2.Unmarshal(network.File(fixtureregistry.FixtureRegistryURL+"index.json"), &catalog); err != nil {
		t.Fatal(err)
	}
	first, _ := publishPackageFixture(t, network, fixtureregistry.FixturePluginName+"/wasm", "v1.0.0", false)
	second, _ := publishPackageFixture(t, network, fixtureregistry.FixturePluginName+"/process", "v2.0.0", true)
	for url, data := range legacyFiles {
		network.SetFile(url, data)
	}
	catalog.Plugins = append(catalog.Plugins, first, second)
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	network.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", data)
	client, err := New(fixtureregistry.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	file, err := OpenLock(t.Context(), filepath.Join(t.TempDir(), LockName))
	if err != nil {
		t.Fatal(err)
	}
	for _, selected := range []struct{ name, version string }{{fixtureregistry.FixturePluginName, "v1.0.0"}, {first.Name, "v1.0.0"}, {second.Name, "v2.0.0"}} {
		pin, err := client.Resolve(t.Context(), selected.name, "latest")
		if err != nil || pin.Name != selected.name || pin.Version != selected.version || pin.RepositoryName() != fixtureregistry.FixturePluginName {
			t.Fatalf("resolve: %+v, %v", pin, err)
		}
		ref, err := pluginref.New(selected.name, selected.version, fixtureregistry.FixtureRegistryURL)
		if err != nil || !pin.Matches(ref) {
			t.Fatalf("reference binding: %v", err)
		}
		if err := file.Set(pin); err != nil {
			t.Fatal(err)
		}
		lookup, err := client.Lookup(t.Context(), selected.name, selected.version)
		if err != nil || !lookup.SameRelease(pin) {
			t.Fatalf("package cache: %+v, %v", lookup, err)
		}
	}
	if err := file.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	reloaded, err := OpenLock(t.Context(), file.Path)
	if err != nil || len(reloaded.Lock.Plugins) != 3 {
		t.Fatalf("lock: %+v, %v", reloaded, err)
	}
	if err := reloaded.Lock.Sync(t.Context(), artifacts.Options{CacheDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []CatalogPlugin{first, second} {
		versions, err := client.Versions(t.Context(), p.Name)
		if err != nil || len(versions) != 1 {
			t.Fatalf("package versions alias: %+v, %v", versions, err)
		}
	}
	catalog.Plugins = []CatalogPlugin{first, second}
	data, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	network.SetFile(fixtureregistry.FixtureRegistryURL+"index.json", data)
	if _, err := client.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest"); err == nil {
		t.Fatal("bare repository silently chose a package")
	}
}

func TestPackageCatalogueRejectsIdentitySubstitution(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for name, mutate := range map[string]func(*manifest, *record){
		"repository":          func(m *manifest, _ *record) { m.Plugin.Repository = "attacker/plugin" },
		"repository-ID":       func(m *manifest, _ *record) { m.Plugin.RepositoryID = "11" },
		"metadata-ID":         func(_ *manifest, r *record) { r.Artifact.PluginID = "other" },
		"metadata-repository": func(_ *manifest, r *record) { r.Artifact.Repository = "https://github.com/attacker/plugin" },
		"receipt":             func(_ *manifest, r *record) { r.Receipt.Identity.RepositoryID = "11" },
		"discriminator":       func(_ *manifest, r *record) { r.IdentityVersion = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			network := fixtureregistry.NewPluginRegistry(t)
			p, recordURL := publishPackageFixture(t, network, fixtureregistry.FixturePluginName+"/wasm", "v1.0.0", false)
			var m manifest
			var r record
			if err := jsonv2.Unmarshal(network.File(fixtureregistry.FixtureRegistryURL+p.Manifest), &m); err != nil {
				t.Fatal(err)
			}
			if err := jsonv2.Unmarshal(network.File(recordURL), &r); err != nil {
				t.Fatal(err)
			}
			mutate(&m, &r)
			for url, value := range map[string]any{fixtureregistry.FixtureRegistryURL + "index.json": Catalog{SchemaVersion: 1, Plugins: []CatalogPlugin{p}}, fixtureregistry.FixtureRegistryURL + p.Manifest: m, recordURL: r} {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				network.SetFile(url, data)
			}
			client, err := New(fixtureregistry.FixtureRegistryURL)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Resolve(t.Context(), p.Name, "v1.0.0"); err == nil {
				t.Fatal("accepted package identity substitution")
			}
		})
	}
}

func TestPackageLockRejectsRepositoryBoundaryChange(t *testing.T) {
	t.Parallel()
	pin := Pin{Registry: DefaultURL, Name: "gitlab.com/publisher/group/repo/package", Repository: "gitlab.com/publisher/group/repo", RepositoryID: "10", OwnerID: "5", PluginID: "gitlab.com/publisher/group/repo/package", Selector: "v1.0.0", Version: "v1.0.0", Kind: artifacts.Component, Asset: "plugin.wasm", URL: "https://gitlab.com/api/v4/projects/10/packages/generic/actionlint-plugin/v1.0.0/plugin.wasm", SHA256: strings.Repeat("a", 64), Contract: "future/1"}
	file, err := OpenLock(t.Context(), filepath.Join(t.TempDir(), LockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Set(pin); err != nil {
		t.Fatal(err)
	}
	changed := pin
	changed.Repository = "gitlab.com/publisher/group"
	if err := changed.Validate(); err != nil {
		t.Fatal("test must isolate replacement binding", err)
	}
	if err := file.Set(changed); err == nil {
		t.Fatal("package's publishing repository boundary changed")
	}
}

func TestCandidateRepositoryCasePreservesIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, id string }{
		{"github.com/publisher/plugin/process", "GitHub.com/Publisher/Plugin/Process"},
		{"github.com/Publisher/Plugin", "legacy"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			metadata := fmt.Appendf(nil, `{"metadata_version":1,"transport":"process","id":%q,"name":"Example","description":"Example plugin","license":"MIT","plugin_version":"1.0.0","repository":"https://github.com/Publisher/Plugin"}`, tc.id)
			body := fmt.Appendf(nil, `{"schema_version":1,"transport":"process","metadata":%s}`, metadata)
			digest := fmt.Sprintf("%x", sha256.Sum256(body))
			declaration, err := json.Marshal(declaration{Kind: artifacts.ProviderManifest, PluginID: tc.id, PluginVersion: "1.0.0", Repository: "https://github.com/Publisher/Plugin", SHA256: digest, Contract: "process/1", Metadata: metadata})
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{
				"index.json":                       fmt.Appendf(nil, `{"schema_version":1,"source":"candidate","plugins":[{"name":%q,"plugin_id":%q,"manifest":"plugins/package/index.json"}]}`, tc.name, tc.id),
				"plugins/package/index.json":       fmt.Appendf(nil, `{"source":"candidate","name":%q,"plugin_id":%q,"versions":[{"version":"v1.0.0","record":"versions/v1.json"}]}`, tc.name, tc.id),
				"plugins/package/versions/v1.json": fmt.Appendf(nil, `{"source":"candidate","artifact":%s,"url":"artifacts/provider.json"}`, declaration),
				"artifacts/provider.json":          body,
			} {
				file := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			u := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(root), "/") + "/"}
			client, err := New(u.String())
			if err != nil {
				t.Fatal(err)
			}
			pin, err := client.Resolve(t.Context(), tc.name, "v1.0.0")
			if err != nil || pin.PluginID != tc.id || !strings.EqualFold(pin.RepositoryName(), fixtureregistry.FixturePluginName) {
				t.Fatalf("candidate case binding: %+v, %v", pin, err)
			}
			lock := Lock{SchemaVersion: 1, Plugins: []Pin{pin}}
			if err := lock.Sync(t.Context(), artifacts.Options{CacheDir: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
