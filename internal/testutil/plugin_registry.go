package testutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

// PluginPublisher is the identity a registry fixture publishes plugin versions
// under. It is a plain value; the registry copies what it keeps.
type PluginPublisher struct {
	// PluginID is the plugin_id written into metadata and catalogue entries.
	PluginID string
	// BaseURL is the registry root serving index.json, statuses.json and records.
	BaseURL string
	// Name is the repository, such as github.com/publisher/plugin.
	Name string
	// Aliases are the catalogue aliases listed for Name.
	Aliases []string
	// RepositoryID is the GitHub repository ID that keys the record paths.
	RepositoryID int
}

// PluginRegistry serves the existing registry export format through a transport,
// so CLI tests exercise HTTPS URLs without opening a server or reaching upstream.
// Every method takes the lock that RoundTrip takes, so tests can change files
// and the offline flag while the code under test is making requests.
type PluginRegistry struct {
	mu        sync.Mutex
	files     map[string][]byte
	requests  []string
	versions  []string
	publisher PluginPublisher
	offline   bool
}

const (
	FixtureRegistryURL    = "https://catalogue.example/"
	FixturePluginName     = "github.com/publisher/plugin"
	fixtureContractDigest = "a0096f562ab3da46b30d0f025ad1c6ed0d46e5b7ea1a53660b3214e3270fcbee"
	emptyStatuses         = `{"schema_version":1,"releases":[]}`
)

// NewPluginRegistry installs a registry for FixturePluginName at
// FixtureRegistryURL as http.DefaultTransport until the test ends.
func NewPluginRegistry(t *testing.T) *PluginRegistry {
	t.Helper()
	IsolateUserCache(t)
	r := &PluginRegistry{
		files:     map[string][]byte{FixtureRegistryURL + "statuses.json": []byte(emptyStatuses)},
		publisher: PluginPublisher{PluginID: "no-sudo", BaseURL: FixtureRegistryURL, Name: FixturePluginName, RepositoryID: 10},
	}
	previous := http.DefaultTransport
	http.DefaultTransport = r
	t.Cleanup(func() { http.DefaultTransport = previous })
	return r
}

// RoundTrip records the request URL and serves the stored file, or 404 when no
// file is stored for the URL. It fails every request while the registry is offline.
func (r *PluginRegistry) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	r.requests = append(r.requests, req.URL.String())
	if r.offline {
		return nil, errors.New("fixture offline")
	}
	data, ok := r.files[req.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	// The body is read after the lock is released, so it gets its own copy
	// instead of sharing storage with r.files.
	body := io.NopCloser(bytes.NewReader(bytes.Clone(data)))
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: body, Request: req}, nil
}

// SetOffline makes RoundTrip fail every request while offline is true.
// Requests are still recorded.
func (r *PluginRegistry) SetOffline(offline bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offline = offline
}

// Requests returns a copy of the URLs requested since the registry was created
// or since the last ResetRequests call.
func (r *PluginRegistry) Requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

// ResetRequests forgets the recorded request URLs.
func (r *PluginRegistry) ResetRequests() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = nil
}

// File returns a copy of the file served at url, or nil when none is stored.
func (r *PluginRegistry) File(url string) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Clone(r.files[url])
}

// Files returns a copy of every served file, keyed by URL.
func (r *PluginRegistry) Files() map[string][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	files := make(map[string][]byte, len(r.files))
	for url, data := range r.files {
		files[url] = bytes.Clone(data)
	}
	return files
}

// SetFile serves a copy of data at url, replacing any stored file.
func (r *PluginRegistry) SetFile(url string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[url] = bytes.Clone(data)
}

// DeleteFile stops serving url, so requests for it get 404.
func (r *PluginRegistry) DeleteFile(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, url)
}

// SetAliases replaces the catalogue aliases that later Add, AddArtifact and
// Publish calls list for the fixture plugin.
func (r *PluginRegistry) SetAliases(aliases ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publisher.Aliases = slices.Clone(aliases)
}

// SetRepository changes the repository name and ID that later Add, AddArtifact
// and Publish calls publish the fixture plugin under. Versions added earlier
// stay in the version list the next Add or AddArtifact publishes.
func (r *PluginRegistry) SetRepository(name string, repositoryID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publisher.Name, r.publisher.RepositoryID = name, repositoryID
}

// RecordURL returns the URL of the fixture plugin's version record for tag.
func (r *PluginRegistry) RecordURL(tag string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return recordURL(r.publisher, tag)
}

// Add publishes tag with wasm as its plugin.wasm asset and returns the served
// artifact bytes.
func (r *PluginRegistry) Add(t *testing.T, tag string, wasm []byte) []byte {
	t.Helper()
	return r.AddArtifact(t, tag, "plugin.wasm", wasm)
}

// AddArtifact publishes one version's selected asset and lists it in the
// catalogue together with every version added before. It returns a copy of the
// served artifact bytes.
func (r *PluginRegistry) AddArtifact(t *testing.T, tag, asset string, wasm []byte) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	data := writeArtifact(r.files, r.publisher, tag, asset, wasm)
	r.versions = append(r.versions, tag)
	writeCatalogue(t, r.files, r.publisher, r.versions...)
	return bytes.Clone(data)
}

// AddPlugin publishes tag of another plugin through the same transport. The
// catalogue at publisher.BaseURL then lists only that plugin and tag, and an
// empty statuses.json is served there unless one already exists. It returns a
// copy of the served artifact bytes.
func (r *PluginRegistry) AddPlugin(t *testing.T, publisher PluginPublisher, tag string, wasm []byte) []byte {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.files[publisher.BaseURL+"statuses.json"]; !ok {
		r.files[publisher.BaseURL+"statuses.json"] = []byte(emptyStatuses)
	}
	data := writeArtifact(r.files, publisher, tag, "plugin.wasm", wasm)
	writeCatalogue(t, r.files, publisher, tag)
	return bytes.Clone(data)
}

// Publish changes catalogue visibility without removing immutable records.
func (r *PluginRegistry) Publish(t *testing.T, versions ...string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	writeCatalogue(t, r.files, r.publisher, versions...)
}

func publisherKey(publisher PluginPublisher) string {
	return fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "https://token.actions.githubusercontent.com\n%d", publisher.RepositoryID)))
}

func recordURL(publisher PluginPublisher, tag string) string {
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(tag)))
	return publisher.BaseURL + "plugins/" + publisherKey(publisher) + "/versions/" + version + ".json"
}

// writeArtifact stores the artifact for tag and its version record in files and
// returns the stored artifact bytes.
func writeArtifact(files map[string][]byte, publisher PluginPublisher, tag, asset string, wasm []byte) []byte {
	repository := strings.TrimPrefix(publisher.Name, "github.com/")
	metadata := fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":%q,"transport":"wasm-component","interface":"kjanat:actionlint/analyzer@0.1.0","contract_sha256":%q}`, publisher.PluginID, strings.TrimPrefix(tag, "v"), "https://"+publisher.Name, fixtureContractDigest)
	kind, contract := "component", "kjanat:actionlint/analyzer@0.1.0#sha256="+fixtureContractDigest
	if len(wasm) >= 8 && bytes.Equal(wasm[4:8], []byte{1, 0, 0, 0}) {
		kind, contract = "core", "wasm-core-json/1"
		metadata = fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":%q,"transport":"wasm-core-json","abi_version":1}`, publisher.PluginID, strings.TrimPrefix(tag, "v"), "https://"+publisher.Name)
	}
	section := binary.AppendUvarint(nil, uint64(len("actionlint.plugin")))
	section = append(section, []byte("actionlint.plugin")...)
	section = append(section, metadata...)
	data := bytes.Clone(wasm)
	data = append(data, 0)
	data = binary.AppendUvarint(data, uint64(len(section)))
	data = append(data, section...)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	artifactURL := "https://" + publisher.Name + "/releases/download/" + tag + "/" + asset
	submission := fmt.Sprintf(`{"schema_version":1,"provider":"github","repository":%q,"tag":%q,"asset":%q,"sha256":%q,"kind":%q}`, repository, tag, asset, digest, kind)
	declaration := fmt.Sprintf(`{"kind":%q,"sha256":%q,"plugin_id":%q,"plugin_version":%q,"repository":%q,"contract":%q,"metadata":%s}`, kind, digest, publisher.PluginID, strings.TrimPrefix(tag, "v"), "https://"+publisher.Name, contract, metadata)
	record := fmt.Sprintf(`{"record_version":2,"artifact":%s,"submission":%s,"receipt":{"schema_version":1,"submission":%s,"identity":{"issuer":"https://token.actions.githubusercontent.com","repository_id":"%d","owner_id":"5","repository":%q,"ref":%q,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"repository_id":%d,"owner_id":5,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","url":%q,"upstream_immutable":true,"release_id":11,"asset_id":12}`, declaration, submission, submission, publisher.RepositoryID, repository, "refs/tags/"+tag, publisher.RepositoryID, artifactURL)
	files[artifactURL] = data
	files[recordURL(publisher, tag)] = []byte(record)
	return data
}

// writeCatalogue stores the catalogue at publisher.BaseURL listing only the
// publisher's plugin, and the plugin manifest listing versions.
func writeCatalogue(t *testing.T, files map[string][]byte, publisher PluginPublisher, versions ...string) {
	t.Helper()
	manifestPath := "plugins/" + publisherKey(publisher) + "/index.json"
	aliases, err := json.Marshal(publisher.Aliases)
	if err != nil {
		t.Fatal(err)
	}
	files[publisher.BaseURL+"index.json"] = fmt.Appendf(nil, `{"schema_version":1,"plugins":[{"name":%q,"aliases":%s,"plugin_id":%q,"manifest":%q}]}`, publisher.Name, aliases, publisher.PluginID, manifestPath)
	links := make([]json.RawMessage, 0, len(versions))
	for _, version := range versions {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(version)))
		links = append(links, fmt.Appendf(nil, `{"version":%q,"record":%q,"compatibility":[]}`, version, "versions/"+key+".json"))
	}
	data, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	files[publisher.BaseURL+manifestPath] = fmt.Appendf(nil, `{"plugin":{"schema_version":1,"issuer":"https://token.actions.githubusercontent.com","repository_id":"%d","owner_id":"5","provider":"github","repository":%q,"plugin_id":%q},"versions":%s}`, publisher.RepositoryID, strings.TrimPrefix(publisher.Name, "github.com/"), publisher.PluginID, data)
}

// NewDefaultPluginRegistry provides both official identities without network or
// guest execution. The minimal component envelopes test installation, not linting.
func NewDefaultPluginRegistry(t *testing.T) *PluginRegistry {
	t.Helper()
	return NewDefaultPluginRegistryWithArtifact(t, []byte{0, 97, 115, 109, 13, 0, 1, 0})
}

// NewDefaultPluginRegistryWithArtifact serves both default identities with the
// supplied component, allowing config inspection to load its rule descriptors.
func NewDefaultPluginRegistryWithArtifact(t *testing.T, wasm []byte) *PluginRegistry {
	t.Helper()
	r := NewPluginRegistry(t)
	const base = "https://plugins.actionlint.kjanat.dev/"
	entries := make([]json.RawMessage, 0, 2)
	for i, name := range []string{"shellcheck", "ruff"} {
		r.AddPlugin(t, PluginPublisher{
			PluginID: name, BaseURL: base, Name: "github.com/kjanat/actionlint-plugin-" + name,
			Aliases: []string{name}, RepositoryID: 10 + i,
		}, "v1.0.0", wasm)
		var catalog struct {
			Plugins []json.RawMessage `json:"plugins"`
		}
		if err := json.Unmarshal(r.File(base+"index.json"), &catalog); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, catalog.Plugins...)
	}
	data, err := json.Marshal(struct {
		SchemaVersion int               `json:"schema_version"`
		Plugins       []json.RawMessage `json:"plugins"`
	}{1, entries})
	if err != nil {
		t.Fatal(err)
	}
	r.SetFile(base+"index.json", data)
	return r
}
