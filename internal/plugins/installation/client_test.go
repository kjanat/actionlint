package installation_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

var emptyComponent = []byte{0, 97, 115, 109, 13, 0, 1, 0}

func fixtureAcquisition(r *fixtureregistry.PluginRegistry) plugininstall.Acquisition {
	return plugininstall.Acquisition{UserCacheDir: r.UserCacheDir(), Connections: r.Connections()}
}

func registryClient(t *testing.T, r *fixtureregistry.PluginRegistry) *plugininstall.Client {
	t.Helper()
	c, err := plugininstall.New(fixtureregistry.FixtureRegistryURL, fixtureAcquisition(r))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRegistrySelection(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	for _, tag := range []string{"v1.9.0", "v1.10.0", "v2.0.0-rc.1", "nightly"} {
		r.Add(t, tag, emptyComponent)
	}
	c := registryClient(t, r)
	for _, tc := range []struct{ selector, want string }{{"latest", "v1.10.0"}, {"nightly", "nightly"}, {"v2.0.0-rc.1", "v2.0.0-rc.1"}} {
		p, err := c.Resolve(t.Context(), fixtureregistry.FixturePluginName, tc.selector)
		if err != nil || p.Version != tc.want || p.Selector != tc.selector {
			t.Fatalf("%s: %+v, %v", tc.selector, p, err)
		}
	}
	r.Publish(t, "v2.0.0-rc.1", "nightly")
	preview, err := c.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || preview.Version != "v2.0.0-rc.1" {
		t.Fatalf("preview-only plugin was not installable: %+v, %v", preview, err)
	}
	r.Add(t, "v2.0.0-rc.2", emptyComponent)
	r.Add(t, "v2.0.0-rc.10", emptyComponent)
	r.Publish(t, "v2.0.0-rc.1", "v2.0.0-rc.2", "v2.0.0-rc.10")
	preview, err = c.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil || preview.Version != "v2.0.0-rc.10" {
		t.Fatalf("latest prerelease was not selected by semantic version: %+v, %v", preview, err)
	}
	for _, versions := range [][]string{{"nightly"}, {"v1.10.0", "1.10.0"}, {"v1.10.0+a", "v1.10.0+b"}} {
		r.Publish(t, versions...)
		if _, err := c.Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest"); err == nil {
			t.Fatalf("ambiguous/non-semantic latest accepted: %v", versions)
		}
	}
	plugins, err := c.Search(t.Context(), "sudo")
	if err != nil || len(plugins) != 1 {
		t.Fatalf("search: %+v %v", plugins, err)
	}
}

func TestRegistryRejectsMalformedResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, before, after string
		index               bool
	}{
		{"duplicate JSON", `"record_version":2`, `"record_version":2,"record_version":2`, false},
		{"digest mismatch", `"sha256":"`, `"sha256":"0`, false},
		{"wrong tag", `"tag":"v1.0.0"`, `"tag":"v2.0.0"`, false},
		{"wrong owner", `"owner_id":5`, `"owner_id":6`, false},
		{"wrong metadata ID", `"id":"no-sudo"`, `"id":"impostor"`, false},
		{"unknown record", `"record_version":2`, `"record_version":3`, false},
		{"remote candidate record", `"record_version":2`, `"record_version":2,"source":"candidate"`, false},
		{"remote candidate catalogue", `"schema_version":1`, `"schema_version":1,"source":"candidate"`, true},
		{"wrong artifact URL", `https://github.com/publisher/plugin/releases/`, `https://attacker.example/releases/`, false},
		{"cross origin manifest", `"manifest":"plugins/`, `"manifest":"https://attacker.example/plugins/`, true},
		{"path escape", `"manifest":"plugins/`, `"manifest":"../plugins/`, true},
		{"unsupported schema", `"schema_version":1`, `"schema_version":2`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := fixtureregistry.NewPluginRegistry(t)
			r.Add(t, "v1.0.0", emptyComponent)
			url := r.RecordURL("v1.0.0")
			if tc.index {
				url = fixtureregistry.FixtureRegistryURL + "index.json"
			}
			r.SetFile(url, bytes.Replace(r.File(url), []byte(tc.before), []byte(tc.after), 1))
			if _, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err == nil {
				t.Fatal("accepted inconsistent registry response")
			}
		})
	}
}

func TestRegistryLegacyAndUnknownContract(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	url := r.RecordURL("v1.0.0")
	data := bytes.ReplaceAll(r.File(url), []byte("kjanat:actionlint/analyzer@0.1.0"), []byte("vendor:future/analyzer@9.0.0"))
	data = bytes.Replace(data, []byte(`"metadata_version":1`), []byte(`"metadata_version":1,"abi_version":{"vendor":"extension"}`), 1)
	r.SetFile(url, data)
	if _, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatalf("admission coupled to host: %v", err)
	}
	data = bytes.Replace(data, []byte(`"record_version":2,`), nil, 1)
	r.SetFile(url, bytes.Replace(data, []byte(`"artifact":`), []byte(`"validation":`), 1))
	if _, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatalf("legacy: %v", err)
	}
}

func TestPinnedSyncVerifiesOfflineArtifacts(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	pin, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	selected := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{pin}}
	options := artifacts.Options{CacheDir: t.TempDir(), Connections: r.Connections()}
	statuses := plugininstall.StatusOptions{Acquisition: fixtureAcquisition(r)}
	if err := selected.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	r.SetOffline(true)
	requests := len(r.Requests())
	if err := selected.Sync(t.Context(), options, statuses); err != nil {
		t.Fatal(err)
	}
	if len(r.Requests()) != requests {
		t.Fatal("offline sync contacted network")
	}
	if err := os.WriteFile(filepath.Join(options.CacheDir, pin.SHA256), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := selected.Sync(t.Context(), options, statuses); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("corrupt cache: %v", err)
	}
}

func TestResolvedPluginValidation(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	p, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*plugininstall.Pin)
	}{
		{"URL", func(p *plugininstall.Pin) { p.URL = "https://internal.example/secret" }},
		{"selector", func(p *plugininstall.Pin) { p.Selector = "v2.0.0" }},
		{"hash", func(p *plugininstall.Pin) { p.SHA256 = "bad" }},
		{"registry", func(p *plugininstall.Pin) { p.Registry = "http://example.com/" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate := p
			tc.edit(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid pin accepted")
			}
		})
	}
	lock := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{p, p}}
	if err := lock.Validate(); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestCompressedRegistryArtifact(t *testing.T) {
	t.Parallel()
	r := fixtureregistry.NewPluginRegistry(t)
	raw := r.Add(t, "v1.0.0", emptyComponent)
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	oldHash := fmt.Sprintf("%x", sha256.Sum256(raw))
	newHash := fmt.Sprintf("%x", sha256.Sum256(compressed.Bytes()))
	for url, data := range r.Files() {
		if strings.HasPrefix(url, fixtureregistry.FixtureRegistryURL) {
			data = bytes.ReplaceAll(data, []byte("plugin.wasm"), []byte("plugin.wasm.gz"))
			r.SetFile(url, bytes.ReplaceAll(data, []byte(oldHash), []byte(newHash)))
		}
	}
	r.SetFile("https://"+fixtureregistry.FixturePluginName+"/releases/download/v1.0.0/plugin.wasm.gz", compressed.Bytes())
	p, err := registryClient(t, r).Resolve(t.Context(), fixtureregistry.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if p.SHA256 != newHash {
		t.Fatal("digest must cover distributed bytes")
	}
	lock := plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{p}}
	if err := lock.Sync(t.Context(), artifacts.Options{CacheDir: t.TempDir(), Connections: r.Connections()}, plugininstall.StatusOptions{Acquisition: fixtureAcquisition(r)}); err != nil {
		t.Fatal(err)
	}
}

func TestGitLabCorePin(t *testing.T) {
	t.Parallel()
	p := plugininstall.Pin{
		Registry: plugininstall.DefaultURL, Name: "gitlab.com/backend.services/team.tools/plugin",
		RepositoryID: "10", OwnerID: "5", PluginID: "vendor/engine", Selector: "v1.0.0", Version: "v1.0.0",
		Kind: artifacts.Core, Asset: "plugin.wasm", SHA256: strings.Repeat("a", 64), Contract: "wasm-core-json/9",
		URL: "https://gitlab.com/api/v4/projects/10/packages/generic/actionlint-plugin/v1.0.0/plugin.wasm",
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	// Future ABI admission is independent of this host's support; unrelated
	// component-field names can carry arbitrary vendor metadata in a core file.
	info := artifacts.Info{Kind: artifacts.Core, SHA256: p.SHA256, Metadata: json.RawMessage(`{"metadata_version":1,"id":"vendor/engine","plugin_version":"1.0.0","repository":"https://gitlab.com/backend.services/team.tools/plugin","transport":"wasm-core-json","abi_version":9,"interface":{"vendor":"extension"},"wit_sha256":false}`)}
	if err := p.Verify(info); err != nil {
		t.Fatal(err)
	}
	p.URL = "https://gitlab.com/group/subgroup/plugin/releases/download/v1.0.0/plugin.wasm"
	if err := p.Validate(); err == nil {
		t.Fatal("accepted noncanonical GitLab download URL")
	}
}

func TestGitHubPinWithSlashTag(t *testing.T) {
	t.Parallel()
	p := plugininstall.Pin{
		Registry: plugininstall.DefaultURL, Name: "github.com/owner/.github",
		RepositoryID: "10", OwnerID: "5", PluginID: "vendor/engine", Selector: "area/v1.2.3", Version: "area/v1.2.3",
		Kind: artifacts.Core, Asset: "plugin.wasm", SHA256: strings.Repeat("a", 64), Contract: "wasm-core-json/9",
		URL: "https://github.com/owner/.github/releases/download/area%2Fv1.2.3/plugin.wasm",
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}
