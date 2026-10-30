package testutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// PluginRegistry serves the existing registry export format through a transport,
// so CLI tests exercise HTTPS URLs without opening a server or reaching upstream.
type PluginRegistry struct {
	mu           sync.Mutex
	Files        map[string][]byte
	Offline      bool
	Requests     []string
	versions     []string
	PluginID     string
	BaseURL      string
	Name         string
	Aliases      []string
	RepositoryID int
}

const (
	FixtureRegistryURL    = "https://catalogue.example/"
	FixturePluginName     = "github.com/publisher/plugin"
	fixtureContractDigest = "a0096f562ab3da46b30d0f025ad1c6ed0d46e5b7ea1a53660b3214e3270fcbee"
)

func NewPluginRegistry(t *testing.T) *PluginRegistry {
	t.Helper()
	IsolateUserCache(t)
	r := &PluginRegistry{Files: map[string][]byte{
		FixtureRegistryURL + "statuses.json": []byte(`{"schema_version":1,"releases":[]}`),
	}, PluginID: "no-sudo", BaseURL: FixtureRegistryURL, Name: FixturePluginName, RepositoryID: 10}
	previous := http.DefaultTransport
	http.DefaultTransport = r
	t.Cleanup(func() { http.DefaultTransport = previous })
	return r
}

func (r *PluginRegistry) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	r.Requests = append(r.Requests, req.URL.String())
	if r.Offline {
		return nil, errors.New("fixture offline")
	}
	data, ok := r.Files[req.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: req}, nil
}

func (r *PluginRegistry) RecordURL(tag string) string {
	key := fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "https://token.actions.githubusercontent.com\n%d", r.RepositoryID)))
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(tag)))
	return r.BaseURL + "plugins/" + key + "/versions/" + version + ".json"
}

func (r *PluginRegistry) Add(t *testing.T, tag string, wasm []byte) []byte {
	t.Helper()
	return r.AddArtifact(t, tag, "plugin.wasm", wasm)
}

// AddArtifact publishes one version's selected asset.
func (r *PluginRegistry) AddArtifact(t *testing.T, tag, asset string, wasm []byte) []byte {
	t.Helper()
	repository := strings.TrimPrefix(r.Name, "github.com/")
	metadata := fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":%q,"transport":"wasm-component","interface":"kjanat:actionlint/analyzer@0.1.0","contract_sha256":%q}`, r.PluginID, strings.TrimPrefix(tag, "v"), "https://"+r.Name, fixtureContractDigest)
	kind, contract := "component", "kjanat:actionlint/analyzer@0.1.0#sha256="+fixtureContractDigest
	if len(wasm) >= 8 && bytes.Equal(wasm[4:8], []byte{1, 0, 0, 0}) {
		kind, contract = "core", "wasm-core-json/1"
		metadata = fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":%q,"transport":"wasm-core-json","abi_version":1}`, r.PluginID, strings.TrimPrefix(tag, "v"), "https://"+r.Name)
	}
	section := binary.AppendUvarint(nil, uint64(len("actionlint.plugin")))
	section = append(section, []byte("actionlint.plugin")...)
	section = append(section, metadata...)
	data := bytes.Clone(wasm)
	data = append(data, 0)
	data = binary.AppendUvarint(data, uint64(len(section)))
	data = append(data, section...)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	artifactURL := "https://" + r.Name + "/releases/download/" + tag + "/" + asset
	submission := fmt.Sprintf(`{"schema_version":1,"provider":"github","repository":%q,"tag":%q,"asset":%q,"sha256":%q,"kind":%q}`, repository, tag, asset, digest, kind)
	declaration := fmt.Sprintf(`{"kind":%q,"sha256":%q,"plugin_id":%q,"plugin_version":%q,"repository":%q,"contract":%q,"metadata":%s}`, kind, digest, r.PluginID, strings.TrimPrefix(tag, "v"), "https://"+r.Name, contract, metadata)
	record := fmt.Sprintf(`{"record_version":2,"artifact":%s,"submission":%s,"receipt":{"schema_version":1,"submission":%s,"identity":{"issuer":"https://token.actions.githubusercontent.com","repository_id":"%d","owner_id":"5","repository":%q,"ref":%q,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"repository_id":%d,"owner_id":5,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","url":%q,"upstream_immutable":true,"release_id":11,"asset_id":12}`, declaration, submission, submission, r.RepositoryID, repository, "refs/tags/"+tag, r.RepositoryID, artifactURL)
	r.Files[artifactURL] = data
	r.Files[r.RecordURL(tag)] = []byte(record)
	r.versions = append(r.versions, tag)
	r.Publish(t, r.versions...)
	return data
}

// Publish changes catalogue visibility without removing immutable records.
func (r *PluginRegistry) Publish(t *testing.T, versions ...string) {
	t.Helper()
	key := fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "https://token.actions.githubusercontent.com\n%d", r.RepositoryID)))
	manifestPath := "plugins/" + key + "/index.json"
	aliases, err := json.Marshal(r.Aliases)
	if err != nil {
		t.Fatal(err)
	}
	r.Files[r.BaseURL+"index.json"] = fmt.Appendf(nil, `{"schema_version":1,"plugins":[{"name":%q,"aliases":%s,"plugin_id":%q,"manifest":%q}]}`, r.Name, aliases, r.PluginID, manifestPath)
	links := make([]json.RawMessage, 0, len(versions))
	for _, version := range versions {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(version)))
		links = append(links, fmt.Appendf(nil, `{"version":%q,"record":%q,"compatibility":[]}`, version, "versions/"+key+".json"))
	}
	data, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	r.Files[r.BaseURL+manifestPath] = fmt.Appendf(nil, `{"plugin":{"schema_version":1,"issuer":"https://token.actions.githubusercontent.com","repository_id":"%d","owner_id":"5","provider":"github","repository":%q,"plugin_id":%q},"versions":%s}`, r.RepositoryID, strings.TrimPrefix(r.Name, "github.com/"), r.PluginID, data)
}

// NewDefaultPluginRegistry serves an official plugin identity for each of
// aliases, in order, without network or guest execution. The minimal component
// envelopes test installation, not linting.
func NewDefaultPluginRegistry(t *testing.T, aliases ...string) *PluginRegistry {
	t.Helper()
	return NewDefaultPluginRegistryWithArtifact(t, []byte{0, 97, 115, 109, 13, 0, 1, 0}, aliases...)
}

// NewDefaultPluginRegistryWithArtifact serves an official plugin identity for
// each of aliases, in order, with the supplied component, allowing config
// inspection to load its rule descriptors. The identity at index i has
// repository ID 10+i.
func NewDefaultPluginRegistryWithArtifact(t *testing.T, wasm []byte, aliases ...string) *PluginRegistry {
	t.Helper()
	r := NewPluginRegistry(t)
	const base = "https://plugins.actionlint.kjanat.dev/"
	entries := make([]json.RawMessage, 0, len(aliases))
	for i, name := range aliases {
		part := &PluginRegistry{
			Files: map[string][]byte{}, BaseURL: base,
			Name: "github.com/kjanat/actionlint-plugin-" + name, Aliases: []string{name}, RepositoryID: 10 + i, PluginID: name,
		}
		part.Add(t, "v1.0.0", wasm)
		maps.Copy(r.Files, part.Files)
		var catalog struct {
			Plugins []json.RawMessage `json:"plugins"`
		}
		if err := json.Unmarshal(part.Files[base+"index.json"], &catalog); err != nil {
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
	r.Files[base+"index.json"] = data
	r.Files[base+"statuses.json"] = []byte(`{"schema_version":1,"releases":[]}`)
	return r
}
