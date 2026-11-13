package oci_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/ocitest"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
)

// The package embeds both its platform manifest and primary layer. A registry
// serves only the root; correct consumers need no separate child downloads.
func TestEmbeddedOCIManifestsAndPrimaryLayers(t *testing.T) {
	t.Parallel()
	for _, cache := range []bool{false, true} {
		for _, forged := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache=%t/forged=%t", cache, forged), func(t *testing.T) {
				t.Parallel()
				registry := ocitest.New(t)
				primaryBytes := []byte(`{"schema_version":1,"transport":"process"}`)
				primary := registry.Blob(artifacts.MediaTypeProviderManifest, primaryBytes)
				type embedded struct {
					ocitest.Descriptor
					Data []byte `json:"data"`
				}
				inline := primaryBytes
				if forged {
					inline = bytes.Repeat([]byte("x"), len(primaryBytes))
				}
				manifestBytes, err := json.Marshal(struct {
					SchemaVersion int                `json:"schemaVersion"`
					MediaType     string             `json:"mediaType"`
					Config        ocitest.Descriptor `json:"config"`
					Layers        []embedded         `json:"layers"`
				}{2, ocitest.ManifestMediaType, registry.Blob("application/vnd.oci.empty.v1+json", []byte("{}")), []embedded{{primary, inline}}})
				if err != nil {
					t.Fatal(err)
				}
				manifest := registry.Blob(ocitest.ManifestMediaType, manifestBytes)
				osName, arch, _ := strings.Cut(artifacts.HostPlatform(), "/")
				manifest.Platform = &ocitest.Platform{OS: osName, Architecture: arch}
				indexBytes, err := json.Marshal(struct {
					SchemaVersion int        `json:"schemaVersion"`
					MediaType     string     `json:"mediaType"`
					Manifests     []embedded `json:"manifests"`
				}{2, ocitest.IndexMediaType, []embedded{{manifest, manifestBytes}}})
				if err != nil {
					t.Fatal(err)
				}
				root := registry.Blob(ocitest.IndexMediaType, indexBytes)
				cacheDir := ""
				if cache {
					cacheDir = t.TempDir()
				}
				source := registry.Reference(root)
				pulled, err := artifacts.Resolve(t.Context(), source, options(t, source, cacheDir))
				if forged {
					if err == nil {
						t.Fatal("forged embedded layer accepted")
					}
					return
				}
				if err != nil || !bytes.Equal(pulled.Bytes, primaryBytes) || pulled.Info.SHA256 != root.Hex() {
					t.Fatalf("embedded package: %+v, %v", pulled, err)
				}
				if registry.Fetches(manifest) != 0 || registry.Fetches(primary) != 0 {
					t.Fatal("embedded content was downloaded separately")
				}
				registry.Close()
				if cache {
					if _, err := artifacts.Resolve(t.Context(), source, options(t, source, cacheDir)); err != nil {
						t.Fatalf("embedded package failed offline: %v", err)
					}
				}
			})
		}
	}
}
