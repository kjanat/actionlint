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
	"strings"
	"sync"
	"testing"
)

// PluginRegistry serves the existing registry export format through a transport,
// so CLI tests exercise HTTPS URLs without opening a server or reaching upstream.
type PluginRegistry struct {
	mu       sync.Mutex
	Files    map[string][]byte
	Offline  bool
	Requests []string
	versions []string
	PluginID string
}

const (
	FixtureRegistryURL    = "https://catalogue.example/"
	FixturePluginName     = "github.com/publisher/plugin"
	fixtureContractDigest = "a0096f562ab3da46b30d0f025ad1c6ed0d46e5b7ea1a53660b3214e3270fcbee"
)

func NewPluginRegistry(t *testing.T) *PluginRegistry {
	t.Helper()
	r := &PluginRegistry{Files: map[string][]byte{}, PluginID: "no-sudo"}
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
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("https://token.actions.githubusercontent.com\n10")))
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(tag)))
	return FixtureRegistryURL + "plugins/" + key + "/versions/" + version + ".json"
}

func (r *PluginRegistry) Add(t *testing.T, tag string, wasm []byte) []byte {
	t.Helper()
	metadata := fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":"https://github.com/publisher/plugin","transport":"wasm-component","interface":"kjanat:actionlint/analyzer@0.1.0","contract_sha256":%q}`, r.PluginID, strings.TrimPrefix(tag, "v"), fixtureContractDigest)
	section := binary.AppendUvarint(nil, uint64(len("actionlint.plugin")))
	section = append(section, []byte("actionlint.plugin")...)
	section = append(section, metadata...)
	data := bytes.Clone(wasm)
	data = append(data, 0)
	data = binary.AppendUvarint(data, uint64(len(section)))
	data = append(data, section...)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	artifactURL := "https://" + FixturePluginName + "/releases/download/" + tag + "/plugin.wasm"
	submission := fmt.Sprintf(`{"schema_version":1,"provider":"github","repository":"publisher/plugin","tag":%q,"asset":"plugin.wasm","sha256":%q,"kind":"component"}`, tag, digest)
	declaration := fmt.Sprintf(`{"kind":"component","sha256":%q,"plugin_id":%q,"plugin_version":%q,"repository":"https://github.com/publisher/plugin","contract":%q,"metadata":%s}`, digest, r.PluginID, strings.TrimPrefix(tag, "v"), "kjanat:actionlint/analyzer@0.1.0#sha256="+fixtureContractDigest, metadata)
	record := fmt.Sprintf(`{"record_version":2,"artifact":%s,"submission":%s,"receipt":{"schema_version":1,"submission":%s,"identity":{"issuer":"https://token.actions.githubusercontent.com","repository_id":"10","owner_id":"5","repository":"publisher/plugin","ref":%q,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"repository_id":10,"owner_id":5,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","url":%q,"upstream_immutable":true,"release_id":11,"asset_id":12}`, declaration, submission, submission, "refs/tags/"+tag, artifactURL)
	r.Files[artifactURL] = data
	r.Files[r.RecordURL(tag)] = []byte(record)
	r.versions = append(r.versions, tag)
	r.Publish(t, r.versions...)
	return data
}

// Publish changes catalogue visibility without removing immutable records.
func (r *PluginRegistry) Publish(t *testing.T, versions ...string) {
	t.Helper()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("https://token.actions.githubusercontent.com\n10")))
	manifestPath := "plugins/" + key + "/index.json"
	r.Files[FixtureRegistryURL+"index.json"] = fmt.Appendf(nil, `{"schema_version":1,"plugins":[{"name":%q,"plugin_id":%q,"manifest":%q}]}`, FixturePluginName, r.PluginID, manifestPath)
	links := make([]json.RawMessage, 0, len(versions))
	for _, version := range versions {
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(version)))
		links = append(links, fmt.Appendf(nil, `{"version":%q,"record":%q,"compatibility":[]}`, version, "versions/"+key+".json"))
	}
	data, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	r.Files[FixtureRegistryURL+manifestPath] = fmt.Appendf(nil, `{"plugin":{"schema_version":1,"issuer":"https://token.actions.githubusercontent.com","repository_id":"10","owner_id":"5","provider":"github","repository":"publisher/plugin","plugin_id":%q,"asset":"plugin.wasm","kind":"component"},"versions":%s}`, r.PluginID, data)
}
