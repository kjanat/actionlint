// Package ocitest serves an OCI distribution registry for tests. It imports
// nothing from actionlint, so the artifact package's own tests can use it.
package ocitest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	IndexMediaType    = "application/vnd.oci.image.index.v1+json"
	ManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
	emptyMediaType    = "application/vnd.oci.empty.v1+json"
	Repository        = "actionlint/provider"
	token             = "fixture-anonymous-token"
)

type Descriptor struct {
	MediaType string    `json:"mediaType"`
	Digest    string    `json:"digest"`
	Size      int64     `json:"size"`
	Platform  *Platform `json:"platform,omitempty"`
}

type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

func (d Descriptor) Hex() string { return strings.TrimPrefix(d.Digest, "sha256:") }

// Registry is an OCI distribution server on a loopback httptest server.
// Every request needs the anonymous bearer token, which /token hands out
// after a 401 challenge, as GHCR does for public images.
type Registry struct {
	host     string
	server   *httptest.Server
	mu       sync.Mutex
	content  map[string][]byte
	types    map[string]string
	served   map[string][]byte
	tags     map[string]string
	requests map[string]int
}

func New(tb testing.TB) *Registry {
	tb.Helper()
	r := &Registry{content: map[string][]byte{}, types: map[string]string{}, served: map[string][]byte{}, tags: map[string]string{}, requests: map[string]int{}}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	tb.Cleanup(r.server.Close)
	r.host = strings.TrimPrefix(r.server.URL, "http://")
	return r
}

func (r *Registry) Host() string { return r.host }

func (r *Registry) Close() { r.server.Close() }

func (r *Registry) Blob(mediaType string, data []byte) Descriptor {
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	r.mu.Lock()
	defer r.mu.Unlock()
	r.content[digest] = append([]byte(nil), data...)
	r.types[digest] = mediaType
	return Descriptor{MediaType: mediaType, Digest: digest, Size: int64(len(data))}
}

func (r *Registry) Manifest(tb testing.TB, layers ...Descriptor) Descriptor {
	tb.Helper()
	config := r.Blob(emptyMediaType, []byte("{}"))
	return r.document(tb, ManifestMediaType, map[string]any{"schemaVersion": 2, "mediaType": ManifestMediaType, "config": config, "layers": layers})
}

// Index stores an image index with one entry per platform "<os>/<arch>".
func (r *Registry) Index(tb testing.TB, platforms map[string]Descriptor) Descriptor {
	tb.Helper()
	entries := make([]Descriptor, 0, len(platforms))
	for platform, manifest := range platforms {
		os, arch, _ := strings.Cut(platform, "/")
		manifest.Platform = &Platform{Architecture: arch, OS: os}
		entries = append(entries, manifest)
	}
	return r.document(tb, IndexMediaType, map[string]any{"schemaVersion": 2, "mediaType": IndexMediaType, "manifests": entries})
}

func (r *Registry) document(tb testing.TB, mediaType string, document map[string]any) Descriptor {
	tb.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		tb.Fatal(err)
	}
	return r.Blob(mediaType, data)
}

func (r *Registry) Tag(tag string, d Descriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tags[tag] = d.Digest
}

func (r *Registry) Reference(d Descriptor) string {
	return "oci://" + r.host + "/" + Repository + "@" + d.Digest
}

// Tamper serves data in place of the content of d, under d's digest.
func (r *Registry) Tamper(d Descriptor, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.served[d.Digest] = append([]byte(nil), data...)
}

func (r *Registry) Fetches(d Descriptor) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[d.Digest]
}

func (r *Registry) serve(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/token" {
		if _, _, ok := req.BasicAuth(); ok {
			http.Error(w, "the fixture is anonymous", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + token + `"}`))
		return
	}
	if req.Header.Get("Authorization") != "Bearer "+token {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.host+`/token",service="fixture",scope="repository:`+Repository+`:pull"`)
		http.Error(w, `{"errors":[{"code":"UNAUTHORIZED"}]}`, http.StatusUnauthorized)
		return
	}
	rest, ok := strings.CutPrefix(req.URL.Path, "/v2/"+Repository+"/")
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}
	kind, reference, _ := strings.Cut(rest, "/")
	r.mu.Lock()
	digest := reference
	if tagged, ok := r.tags[reference]; ok && kind == "manifests" {
		digest = tagged
	}
	data, found := r.content[digest]
	mediaType := r.types[digest]
	if served, ok := r.served[digest]; ok {
		data = served
	}
	if found && req.Method == http.MethodGet {
		r.requests[digest]++
	}
	r.mu.Unlock()
	if !found || kind != "manifests" && kind != "blobs" {
		http.Error(w, `{"errors":[{"code":"NOT_FOUND"}]}`, http.StatusNotFound)
		return
	}
	if kind == "manifests" {
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", digest)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if req.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
