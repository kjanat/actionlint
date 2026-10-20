package pluginregistry_test

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

	"actionlint.kjanat.dev/v2/internal/pluginregistry"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

var emptyComponent = []byte{0, 97, 115, 109, 13, 0, 1, 0}

func registryClient(t *testing.T) *pluginregistry.Client {
	t.Helper()
	c, err := pluginregistry.New(testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRegistrySelection(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	for _, tag := range []string{"v1.9.0", "v1.10.0", "v2.0.0-rc.1", "nightly"} {
		r.Add(t, tag, emptyComponent)
	}
	c := registryClient(t)
	for _, tc := range []struct{ selector, want string }{{"latest", "v1.10.0"}, {"nightly", "nightly"}, {"v2.0.0-rc.1", "v2.0.0-rc.1"}} {
		p, err := c.Resolve(t.Context(), testutil.FixturePluginName, tc.selector)
		if err != nil || p.Version != tc.want || p.Selector != tc.selector {
			t.Fatalf("%s: %+v, %v", tc.selector, p, err)
		}
	}
	for _, versions := range [][]string{{"v2.0.0-rc.1", "nightly"}, {"v1.10.0", "1.10.0"}, {"v1.10.0+a", "v1.10.0+b"}} {
		r.Publish(t, versions...)
		if _, err := c.Resolve(t.Context(), testutil.FixturePluginName, "latest"); err == nil {
			t.Fatalf("ambiguous/nonstable latest accepted: %v", versions)
		}
	}
	plugins, err := c.Search(t.Context(), "sudo")
	if err != nil || len(plugins) != 1 {
		t.Fatalf("search: %+v %v", plugins, err)
	}
}

func TestRegistryRejectsMalformedResponses(t *testing.T) {
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
		{"wrong artifact URL", `https://github.com/publisher/plugin/releases/`, `https://attacker.example/releases/`, false},
		{"cross origin manifest", `"manifest":"plugins/`, `"manifest":"https://attacker.example/plugins/`, true},
		{"path escape", `"manifest":"plugins/`, `"manifest":"../plugins/`, true},
		{"unsupported schema", `"schema_version":1`, `"schema_version":2`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := testutil.NewPluginRegistry(t)
			r.Add(t, "v1.0.0", emptyComponent)
			url := r.RecordURL("v1.0.0")
			if tc.index {
				url = testutil.FixtureRegistryURL + "index.json"
			}
			r.Files[url] = bytes.Replace(r.Files[url], []byte(tc.before), []byte(tc.after), 1)
			if _, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0"); err == nil {
				t.Fatal("accepted inconsistent registry response")
			}
		})
	}
}

func TestRegistryLegacyAndUnknownContract(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	url := r.RecordURL("v1.0.0")
	data := bytes.ReplaceAll(r.Files[url], []byte("kjanat:actionlint/analyzer@0.1.0"), []byte("vendor:future/analyzer@9.0.0"))
	data = bytes.Replace(data, []byte(`"metadata_version":1`), []byte(`"metadata_version":1,"abi_version":{"vendor":"extension"}`), 1)
	r.Files[url] = data
	if _, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatalf("admission coupled to host: %v", err)
	}
	data = bytes.Replace(data, []byte(`"record_version":2,`), nil, 1)
	r.Files[url] = bytes.Replace(data, []byte(`"artifact":`), []byte(`"validation":`), 1)
	if _, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "v1.0.0"); err != nil {
		t.Fatalf("legacy: %v", err)
	}
}

func TestPinnedSyncAndLockTransactions(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	p, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), pluginregistry.LockName)
	f, err := pluginregistry.OpenLock(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set(p); err != nil {
		t.Fatal(err)
	}
	options := artifact.Options{CacheDir: t.TempDir()}
	if err := f.Lock.Sync(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	stale, err := pluginregistry.OpenLock(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	r.Offline = true
	requests := len(r.Requests)
	if err := stale.Lock.Sync(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	if len(r.Requests) != requests {
		t.Fatal("offline sync contacted network")
	}
	changed := p
	changed.SHA256 = strings.Repeat("0", 64)
	if err := f.Set(changed); err == nil {
		t.Fatal("accepted replacement digest for immutable version")
	}
	if err := f.Remove(p.Name); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := stale.Save(t.Context()); err == nil {
		t.Fatal("overwrote concurrent lock edit")
	}
	if err := os.WriteFile(filepath.Join(options.CacheDir, p.SHA256), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := stale.Lock.Sync(t.Context(), options); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("corrupt cache: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil || len(entries) != 1 {
		t.Fatalf("transaction debris: %v %v", entries, err)
	}
}

func TestLockValidation(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
	r.Add(t, "v1.0.0", emptyComponent)
	p, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*pluginregistry.Pin)
	}{
		{"URL", func(p *pluginregistry.Pin) { p.URL = "https://internal.example/secret" }},
		{"selector", func(p *pluginregistry.Pin) { p.Selector = "v2.0.0" }},
		{"hash", func(p *pluginregistry.Pin) { p.SHA256 = "bad" }},
		{"registry", func(p *pluginregistry.Pin) { p.Registry = "http://example.com/" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := p
			tc.edit(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid pin accepted")
			}
		})
	}
	lock := pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{p, p}}
	if err := lock.Validate(); err == nil {
		t.Fatal("duplicate accepted")
	}
	file := filepath.Join(t.TempDir(), pluginregistry.LockName)
	for _, data := range []string{`null`, `{"schema_version":1,"plugins":null}`, `{"schema_version":1,"plugins":[],"typo":true}`, `{"schema_version":1,"plugins":[]} {}`} {
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := pluginregistry.OpenLock(t.Context(), file); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	data, err := json.Marshal(pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{p}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(file), "link.json")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := pluginregistry.OpenLock(t.Context(), link); err == nil {
		t.Fatal("symlink lock accepted")
	}
}

func TestCompressedRegistryArtifact(t *testing.T) {
	r := testutil.NewPluginRegistry(t)
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
	for url, data := range r.Files {
		if strings.HasPrefix(url, testutil.FixtureRegistryURL) {
			data = bytes.ReplaceAll(data, []byte("plugin.wasm"), []byte("plugin.wasm.gz"))
			r.Files[url] = bytes.ReplaceAll(data, []byte(oldHash), []byte(newHash))
		}
	}
	r.Files["https://"+testutil.FixturePluginName+"/releases/download/v1.0.0/plugin.wasm.gz"] = compressed.Bytes()
	p, err := registryClient(t).Resolve(t.Context(), testutil.FixturePluginName, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if p.SHA256 != newHash {
		t.Fatal("digest must cover distributed bytes")
	}
	lock := pluginregistry.Lock{SchemaVersion: 1, Plugins: []pluginregistry.Pin{p}}
	if err := lock.Sync(t.Context(), artifact.Options{CacheDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

func TestGitLabCorePin(t *testing.T) {
	p := pluginregistry.Pin{
		Registry: pluginregistry.DefaultURL, Name: "gitlab.com/group/subgroup/plugin",
		RepositoryID: "10", OwnerID: "5", PluginID: "vendor/engine", Selector: "v1.0.0", Version: "v1.0.0",
		Kind: artifact.Core, Asset: "plugin.wasm", SHA256: strings.Repeat("a", 64), Contract: "wasm-core-json/9",
		URL: "https://gitlab.com/api/v4/projects/10/packages/generic/actionlint-plugin/v1.0.0/plugin.wasm",
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	// Future ABI admission is independent of this host's support; unrelated
	// component-field names can carry arbitrary vendor metadata in a core file.
	info := artifact.Info{Kind: artifact.Core, SHA256: p.SHA256, Metadata: json.RawMessage(`{"metadata_version":1,"id":"vendor/engine","plugin_version":"1.0.0","repository":"https://gitlab.com/group/subgroup/plugin","transport":"wasm-core-json","abi_version":9,"interface":{"vendor":"extension"},"contract_sha256":false}`)}
	if err := p.Verify(info); err != nil {
		t.Fatal(err)
	}
	p.URL = "https://gitlab.com/group/subgroup/plugin/releases/download/v1.0.0/plugin.wasm"
	if err := p.Validate(); err == nil {
		t.Fatal("accepted noncanonical GitLab download URL")
	}
}
