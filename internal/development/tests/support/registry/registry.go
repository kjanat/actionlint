package registry

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"golang.org/x/net/dns/dnsmessage"
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
	// Metadata supplies discovery metadata for fixtures that check it against a runtime export.
	Metadata json.RawMessage
}

// PluginRegistry serves the existing registry export format through a transport,
// so CLI tests exercise HTTPS URLs without opening a server or reaching upstream.
// Every method takes the lock that RoundTrip takes, so tests can change files
// and the offline flag while the code under test is making requests.
type PluginRegistry struct {
	mu           sync.Mutex
	files        map[string][]byte
	pending      map[string][][]byte
	requests     []string
	versions     []string
	publisher    PluginPublisher
	offline      bool
	connections  acquisitionhttp.Connections
	userCacheDir string
}

const (
	FixtureRegistryURL = "https://catalogue.example/"
	FixturePluginName  = "github.com/publisher/plugin"
	fixtureWITSHA256   = "a0096f562ab3da46b30d0f025ad1c6ed0d46e5b7ea1a53660b3214e3270fcbee"
	emptyStatuses      = `{"schema_version":1,"releases":[]}`
)

// NewPluginRegistry serves a registry for FixturePluginName at
// FixtureRegistryURL through Connections until the test ends.
func NewPluginRegistry(t *testing.T) *PluginRegistry {
	t.Helper()
	r := &PluginRegistry{
		files:        map[string][]byte{FixtureRegistryURL + "statuses.json": []byte(emptyStatuses)},
		pending:      make(map[string][][]byte),
		publisher:    PluginPublisher{PluginID: "no-sudo", BaseURL: FixtureRegistryURL, Name: FixturePluginName, RepositoryID: 10},
		userCacheDir: t.TempDir(),
	}
	transport := r.transport(t)
	r.connections = acquisitionhttp.Connections{Transport: transport, Resolver: &net.Resolver{PreferGo: true, Dial: fixtureDNS}}
	t.Cleanup(transport.CloseIdleConnections)
	return r
}

func (r *PluginRegistry) Connections() acquisitionhttp.Connections { return r.connections }

func (r *PluginRegistry) UserCacheDir() string { return r.userCacheDir }

// transport exercises the standard HTTP client's DNS, pinning, TLS and Host
// handling. Only in-memory connections are opened. A fixed-seed Ed25519 CA
// avoids slow RSA generation; dynamically signed leaves preserve real hostname
// verification for every fixture publisher and registry authority.
func (r *PluginRegistry) transport(t *testing.T) *http.Transport {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{37}, ed25519.SeedSize))
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "actionlint fixture CA"},
		NotBefore: time.Unix(0, 0), NotAfter: time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	serverTLS := &tls.Config{GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(2),
			NotBefore:    ca.NotBefore, NotAfter: ca.NotAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if hello.ServerName != "" {
			leaf.DNSNames = []string{hello.ServerName}
		} else {
			// TLS sends no SNI for literal IP authorities. The fixture's
			// explicit-private tests select loopback endpoints only.
			leaf.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
		}
		certificate, err := x509.CreateCertificate(rand.Reader, leaf, ca, key.Public(), key)
		if err != nil {
			return nil, err
		}
		return &tls.Certificate{Certificate: [][]byte{certificate}, PrivateKey: key}, nil
	}}
	return &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots},
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, _, err := net.SplitHostPort(address); err != nil {
				return nil, err
			}
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				connection := tls.Server(server, serverTLS)
				defer connection.Close()
				request, err := http.ReadRequest(bufio.NewReader(connection))
				if err != nil {
					return
				}
				defer request.Body.Close()
				request.URL.Scheme, request.URL.Host = "https", request.Host
				response, err := r.RoundTrip(request)
				if err != nil {
					return
				}
				defer response.Body.Close()
				response.Close = true
				_ = response.Write(connection)
			}()
			return client, nil
		},
	}
}

// fixtureDNS supplies one public numeric answer through the resolver's normal
// DNS protocol. The production transport must validate and pin that answer
// before the fixture dialer handles the connection; no external DNS is used.
func fixtureDNS(ctx context.Context, _, _ string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		var size [2]byte
		if _, err := io.ReadFull(server, size[:]); err != nil {
			return
		}
		packet := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(server, packet); err != nil {
			return
		}
		var query dnsmessage.Message
		if err := query.Unpack(packet); err != nil {
			return
		}
		response := dnsmessage.Message{ID: query.ID, Response: true, RecursionDesired: true, RecursionAvailable: true, Questions: query.Questions}
		for _, question := range query.Questions {
			if question.Type == dnsmessage.TypeA {
				response.Answers = append(response.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{1, 1, 1, 1}}})
			}
		}
		packed, err := response.Pack()
		if err != nil || len(packed) > 65535 {
			return
		}
		binary.BigEndian.PutUint16(size[:], uint16(len(packed)))
		_, _ = server.Write(append(size[:], packed...))
	}()
	return client, nil
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
	if pending := r.pending[req.URL.String()]; len(pending) != 0 {
		r.files[req.URL.String()] = bytes.Clone(pending[0])
		if len(pending) == 1 {
			delete(r.pending, req.URL.String())
		} else {
			r.pending[req.URL.String()] = pending[1:]
		}
	}
	// Copy the response body before releasing the lock. The caller reads it later.
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
	delete(r.pending, url)
}

// SetFileSequence serves first on the next request, then each next value on
// later requests. The last value remains served after the sequence is exhausted.
func (r *PluginRegistry) SetFileSequence(url string, first []byte, next ...[]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.files[url] = bytes.Clone(first)
	if len(next) == 0 {
		delete(r.pending, url)
		return
	}
	sequence := make([][]byte, len(next))
	for i, data := range next {
		sequence[i] = bytes.Clone(data)
	}
	r.pending[url] = sequence
}

// DeleteFile stops serving url, so requests for it get 404.
func (r *PluginRegistry) DeleteFile(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, url)
	delete(r.pending, url)
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
	data := writeArtifact(t, r.files, r.publisher, tag, asset, wasm)
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
	data := writeArtifact(t, r.files, publisher, tag, "plugin.wasm", wasm)
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
func writeArtifact(t *testing.T, files map[string][]byte, publisher PluginPublisher, tag, asset string, wasm []byte) []byte {
	t.Helper()
	format, _, err := artifacts.Sniff(wasm)
	if err != nil {
		t.Fatalf("registry fixture artifact: %v", err)
	}
	repository := strings.TrimPrefix(publisher.Name, "github.com/")
	metadata := fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":%q,"transport":"wasm-component","interface":"kjanat:actionlint/analyzer@0.1.0","wit_sha256":%q}`, publisher.PluginID, strings.TrimPrefix(tag, "v"), "https://"+publisher.Name, fixtureWITSHA256)
	kind, contract := "component", "kjanat:actionlint/analyzer@0.1.0#sha256="+fixtureWITSHA256
	if format == artifacts.Core {
		kind, contract = "core", "wasm-core-json/1"
		metadata = fmt.Sprintf(`{"metadata_version":1,"id":%q,"name":"Test plugin","description":"Registry test","license":"MIT","plugin_version":%q,"repository":%q,"transport":"wasm-core-json","abi_version":1}`, publisher.PluginID, strings.TrimPrefix(tag, "v"), "https://"+publisher.Name)
	}
	if len(publisher.Metadata) != 0 {
		metadata = string(publisher.Metadata)
	}
	section := binary.AppendUvarint(nil, uint64(len("actionlint.plugin")))
	section = append(section, []byte("actionlint.plugin")...)
	section = append(section, metadata...)
	data := bytes.Clone(wasm[:8])
	// Sniff validated the section lengths. Copy complete outer sections so
	// replacing discovery metadata does not alter embedded modules or components.
	for rest := wasm[8:]; len(rest) > 0; {
		original := rest
		id := rest[0]
		size, count := binary.Uvarint(rest[1:])
		body := rest[1+count : 1+count+int(size)]
		rest = rest[1+count+int(size):]
		if id == 0 {
			length, count := binary.Uvarint(body)
			if string(body[count:count+int(length)]) == artifacts.MetadataSection {
				continue
			}
		}
		data = append(data, original[:len(original)-len(rest)]...)
	}
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

// NewDefaultPluginRegistry serves an official plugin identity for each of
// aliases, in order, without network or guest execution. The minimal component
// envelopes support installation tests without running a linter.
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
