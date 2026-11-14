package oci_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/ocitest"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type ociCandidateFixture struct {
	registry *ocitest.Registry
	client   *plugininstall.Client
	dir      string
	record   candidateRecord
	root     ocitest.Descriptor
	manifest ocitest.Descriptor
	primary  ocitest.Descriptor
	options  artifacts.Options
}

// Candidate wire data is built here so the transport test exercises the public
// registry client without importing private catalogue representations.
type candidateRecord struct {
	Source   string      `json:"source"`
	Artifact declaration `json:"artifact"`
	URL      string      `json:"url"`
}

type declaration struct {
	Kind          artifacts.Kind  `json:"kind"`
	SHA256        string          `json:"sha256"`
	PluginID      string          `json:"plugin_id"`
	PluginVersion string          `json:"plugin_version"`
	Repository    string          `json:"repository"`
	Contract      string          `json:"contract"`
	Metadata      json.RawMessage `json:"metadata"`
}

func newOCICandidate(t *testing.T, kind artifacts.Kind) ociCandidateFixture {
	t.Helper()
	cache := t.TempDir()
	registry := ocitest.New(t)
	metadata := []byte(`{"metadata_version":1,"id":"github.com/publisher/plugin/check","plugin_version":"1.0.0","repository":"https://github.com/publisher/plugin"`)
	contract := "process/1"
	mediaType := artifacts.MediaTypeWasm
	var data []byte
	switch kind {
	case artifacts.Core:
		metadata = append(metadata, `,"transport":"wasm-core-json","abi_version":99}`...)
		contract = "wasm-core-json/99"
		data = []byte{0, 97, 115, 109, 1, 0, 0, 0}
	case artifacts.Component:
		metadata = fmt.Appendf(metadata, `,"transport":"wasm-component","interface":"publisher:check/analyzer@9.0.0","wit_sha256":%q}`, strings.Repeat("b", 64))
		contract = "publisher:check/analyzer@9.0.0#sha256=" + strings.Repeat("b", 64)
		data = []byte{0, 97, 115, 109, 13, 0, 1, 0}
	case artifacts.ProviderManifest:
		metadata = append(metadata, `,"transport":"process"}`...)
		mediaType = artifacts.MediaTypeProviderManifest
		data = fmt.Appendf(nil, `{"schema_version":1,"transport":"process","metadata":%s}`, metadata)
	default:
		t.Fatalf("unsupported fixture kind %q", kind)
	}
	if kind != artifacts.ProviderManifest {
		section := binary.AppendUvarint(nil, uint64(len(artifacts.MetadataSection)))
		section = append(section, artifacts.MetadataSection...)
		section = append(section, metadata...)
		data = append(data, 0)
		data = binary.AppendUvarint(data, uint64(len(section)))
		data = append(data, section...)
	}
	primary := registry.Blob(mediaType, data)
	manifest := registry.Manifest(t, primary)
	root := registry.Index(t, map[string]ocitest.Descriptor{artifacts.HostPlatform(): manifest})
	record := candidateRecord{Source: "candidate", Artifact: declaration{
		Kind: kind, SHA256: root.Hex(), PluginID: "github.com/publisher/plugin/check", PluginVersion: "1.0.0",
		Repository: "https://github.com/publisher/plugin", Contract: contract, Metadata: metadata,
	}, URL: registry.Reference(root)}
	dir := t.TempDir()
	for name, value := range map[string]json.RawMessage{
		"index.json":               []byte(`{"schema_version":1,"source":"candidate","plugins":[{"name":"github.com/publisher/plugin/check","plugin_id":"github.com/publisher/plugin/check","manifest":"plugins/check/index.json"}]}`),
		"plugins/check/index.json": []byte(`{"source":"candidate","name":"github.com/publisher/plugin/check","plugin_id":"github.com/publisher/plugin/check","versions":[{"version":"v1.0.0","record":"versions/v1.json"}]}`),
	} {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(dir) + "/"}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	client, err := plugininstall.New(u.String(), plugininstall.Acquisition{UserCacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := artifacts.ExplicitSource(record.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.StatusOptions.CacheDir = filepath.Join(cache, "actionlint", "registry")
	fixture := ociCandidateFixture{
		registry: registry, client: client, dir: dir, record: record, root: root, manifest: manifest, primary: primary,
		options: artifacts.Options{CacheDir: filepath.Join(cache, "actionlint", "artifacts"), PullOCI: oci.Pull, Network: grant},
	}
	fixture.writeRecord(t)
	return fixture
}

func (f ociCandidateFixture) writeRecord(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(f.record)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.dir, "plugins", "check", "versions", "v1.json")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func (f ociCandidateFixture) resolve(t *testing.T) plugininstall.Pin {
	t.Helper()
	pin, err := f.client.Resolve(t.Context(), "github.com/publisher/plugin/check", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return pin
}

func (f ociCandidateFixture) syncCandidate(t *testing.T, pin plugininstall.Pin, options artifacts.Options) error {
	t.Helper()
	return (plugininstall.ResolvedPlugins{Plugins: []plugininstall.Pin{pin}}).Sync(t.Context(), options, f.client.StatusOptions)
}

func (f ociCandidateFixture) cachedOptions() artifacts.Options {
	return artifacts.Options{CacheDir: f.options.CacheDir, PullOCI: oci.Cached}
}

func TestCandidateOCIRootDigestAndMetadataAdmission(t *testing.T) {
	t.Parallel()
	for _, kind := range []artifacts.Kind{artifacts.Core, artifacts.Component, artifacts.ProviderManifest} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			f := newOCICandidate(t, kind)
			pin := f.resolve(t)
			if pin.Source() != f.record.URL || pin.Asset != "" || pin.SHA256 != f.root.Hex() || pin.RepositoryName() != "github.com/publisher/plugin" {
				t.Fatalf("OCI candidate lost its source or identity: %+v", pin)
			}
			if f.registry.Fetches(f.root) != 0 || f.registry.Fetches(f.primary) != 0 {
				t.Fatal("catalogue resolution fetched OCI content")
			}
			if err := f.syncCandidate(t, pin, f.options); err != nil {
				t.Fatal(err)
			}
			if err := f.client.RememberCandidate(t.Context(), pin, f.cachedOptions()); err != nil {
				t.Fatal(err)
			}
			for _, digest := range []string{f.root.Hex(), f.manifest.Hex(), f.primary.Hex()} {
				if _, err := (artifacts.Store{Dir: f.options.CacheDir}).Read(t.Context(), digest, 1<<20); err != nil {
					t.Fatal(err)
				}
			}
			f.registry.Close()
			if err := os.Remove(filepath.Join(f.dir, "plugins", "check", "versions", "v1.json")); err != nil {
				t.Fatal(err)
			}
			remembered, err := f.client.LookupCandidate(t.Context(), pin.Name, pin.Version, pin.SHA256)
			if err != nil || remembered != pin {
				t.Fatalf("offline remembered candidate: %+v, %v", remembered, err)
			}
			if err := f.syncCandidate(t, remembered, f.cachedOptions()); err != nil {
				t.Fatalf("offline OCI reconstruction: %v", err)
			}
		})
	}
}

func TestCandidateOCIRejectsForgedRootAndMetadata(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"root digest", "tag", "noncanonical", "metadata", "primary digest"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			f := newOCICandidate(t, artifacts.ProviderManifest)
			switch mutation {
			case "root digest":
				f.record.Artifact.SHA256 = strings.Repeat("a", 64)
			case "tag":
				f.record.URL = "oci://" + f.registry.Host() + "/actionlint/provider:latest"
			case "noncanonical":
				f.record.URL = strings.Replace(f.record.URL, "oci://", "OCI://", 1)
			case "metadata":
				f.record.Artifact.Metadata = bytes.Replace(f.record.Artifact.Metadata, []byte(`"id":"github.com/publisher/plugin/check"`), []byte(`"id":"other"`), 1)
			case "primary digest":
				f.record.URL = f.registry.Reference(f.primary)
				f.record.Artifact.SHA256 = f.primary.Hex()
			}
			f.writeRecord(t)
			pin, err := f.client.Resolve(t.Context(), "github.com/publisher/plugin/check", "v1.0.0")
			if mutation != "primary digest" {
				if err == nil {
					t.Fatalf("forged candidate resolved: %+v", pin)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := f.syncCandidate(t, pin, f.options); err == nil {
				t.Fatal("primary layer admitted as OCI root")
			}
		})
	}
}

func TestCandidateOCIRememberRequiresCompleteVerifiedCache(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"root", "manifest", "primary", "forged metadata"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			f := newOCICandidate(t, artifacts.ProviderManifest)
			pin := f.resolve(t)
			if err := f.client.RememberCandidate(t.Context(), pin, f.cachedOptions()); err == nil {
				t.Fatal("uncached OCI candidate remembered")
			}
			if err := f.syncCandidate(t, pin, f.options); err != nil {
				t.Fatal(err)
			}
			digest := map[string]string{"root": f.root.Hex(), "manifest": f.manifest.Hex(), "primary": f.primary.Hex()}[missing]
			if missing == "forged metadata" {
				pin.PluginID = "github.com/publisher/plugin/other"
				pin.Name = pin.PluginID
			} else if err := os.Remove(filepath.Join(f.options.CacheDir, digest)); err != nil {
				t.Fatal(err)
			}
			before := f.registry.Fetches(f.root) + f.registry.Fetches(f.manifest) + f.registry.Fetches(f.primary)
			if err := f.client.RememberCandidate(t.Context(), pin, f.cachedOptions()); err == nil {
				t.Fatal("incomplete or inconsistent OCI cache remembered")
			}
			if err := f.syncCandidate(t, pin, f.cachedOptions()); err == nil {
				t.Fatal("incomplete or inconsistent OCI cache restored offline")
			}
			if after := f.registry.Fetches(f.root) + f.registry.Fetches(f.manifest) + f.registry.Fetches(f.primary); after != before {
				t.Fatal("RememberCandidate fetched missing OCI content")
			}
		})
	}
}

func TestCandidateOCIRejectsForgedPrimaryBytes(t *testing.T) {
	t.Parallel()
	f := newOCICandidate(t, artifacts.ProviderManifest)
	pin := f.resolve(t)
	f.registry.Tamper(f.primary, bytes.Repeat([]byte("x"), int(f.primary.Size)))
	if err := f.syncCandidate(t, pin, f.options); err == nil {
		t.Fatal("OCI candidate accepted primary bytes that do not match the root's layer digest")
	}
	if err := f.client.RememberCandidate(t.Context(), pin, f.cachedOptions()); err == nil {
		t.Fatal("OCI candidate remembered an unverified layer")
	}
}

func TestCandidateOCICacheRejectsCorruptionWithoutFetching(t *testing.T) {
	t.Parallel()
	for _, corrupted := range []string{"root", "manifest", "primary"} {
		t.Run(corrupted, func(t *testing.T) {
			t.Parallel()
			f := newOCICandidate(t, artifacts.ProviderManifest)
			pin := f.resolve(t)
			if err := f.syncCandidate(t, pin, f.options); err != nil {
				t.Fatal(err)
			}
			digest := map[string]string{"root": f.root.Hex(), "manifest": f.manifest.Hex(), "primary": f.primary.Hex()}[corrupted]
			if err := os.WriteFile(filepath.Join(f.options.CacheDir, digest), []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
			before := f.registry.Fetches(f.root) + f.registry.Fetches(f.manifest) + f.registry.Fetches(f.primary)
			if err := f.client.RememberCandidate(t.Context(), pin, f.cachedOptions()); !errors.Is(err, artifacts.ErrCorrupt) {
				t.Fatalf("corrupt OCI cache: %v", err)
			}
			if after := f.registry.Fetches(f.root) + f.registry.Fetches(f.manifest) + f.registry.Fetches(f.primary); after != before {
				t.Fatal("corrupt cached candidate triggered a fetch")
			}
		})
	}
}
