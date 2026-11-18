package pluginreference

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "engine.wasm")
	uriPath := filepath.ToSlash(file)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	fileURI := (&url.URL{Scheme: "file", Path: uriPath}).String()
	pin := "#sha256=" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, source, reference, local string
	}{
		{"relative", filepath.Join("engines", "..", "engine.wasm"), file, file},
		{"absolute", file, file, file},
		{"http", "http://example.com/engine?version=1" + pin, "http://example.com/engine?version=1" + pin, ""},
		{"https", "https://example.com/engine?version=1" + pin, "https://example.com/engine?version=1" + pin, ""},
		{"file", fileURI + pin, fileURI + pin, file},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reference, local, err := NormalizeSource(tc.source, root)
			if err != nil || reference != tc.reference || local != tc.local {
				t.Fatalf("NormalizeSource(%q) = (%q, %q, %v), want (%q, %q, nil)", tc.source, reference, local, err, tc.reference, tc.local)
			}
		})
	}
	for _, source := range []string{"", "ftp://example.com/engine", "https://user:secret@example.com/engine", "https://example.com/engine#sha256=invalid"} {
		if _, _, err := NormalizeSource(source, root); err == nil {
			t.Errorf("accepted invalid source %q", source)
		}
	}
}

func TestOCICanonicalHost(t *testing.T) {
	t.Parallel()
	digest := "@sha256:" + strings.Repeat("ab", 32)
	for source, want := range map[string]string{
		"oci://GHCR.io/owner/provider":             "oci://ghcr.io/owner/provider",
		"oci://localhost:05000/owner/provider":     "oci://localhost:5000/owner/provider",
		"oci://ghcr.io:443/owner/provider":         "oci://ghcr.io/owner/provider",
		"oci://ghcr.io:80/owner/provider":          "oci://ghcr.io:80/owner/provider",
		"oci://localhost:80/owner/provider":        "oci://localhost/owner/provider",
		"oci://127.0.0.1:443/owner/provider":       "oci://127.0.0.1:443/owner/provider",
		"oci://[0:0:0:0:0:0:0:1]:5000/owner/image": "oci://[::1]:5000/owner/image",
	} {
		ref, err := ParseOCI(source + digest)
		if err != nil {
			t.Fatalf("%s: %v", source, err)
		}
		if got := ref.String(); got != want+digest {
			t.Errorf("ParseOCI(%q) = %q, want %q", source, got, want+digest)
		}
	}
	for _, source := range []string{"oci://registry.example:65536/owner/provider", "oci://-registry.example/owner/provider"} {
		if _, err := ParseOCI(source + digest); err == nil {
			t.Errorf("accepted OCI host in %q", source)
		}
	}
}

// A cache locator cannot carry another location or a second digest.
func TestCandidateCacheRejectsInvalidURI(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("ab", 32)
	for _, source := range []string{
		"sha256://", "sha256://../outside", "sha256://" + strings.Repeat("a", 63),
		"sha256://" + strings.Repeat("z", 64), "sha256://" + strings.ToUpper(digest),
		"sha256://user@" + digest, "sha256://" + digest + ":443", "sha256://" + digest + "/file",
		"sha256://" + digest + "?", "sha256://" + digest + "?url=https://example.com",
		"sha256://" + digest + "#sha256=" + digest,
	} {
		if _, _, err := NormalizeSource(source, t.TempDir()); err == nil {
			t.Errorf("accepted cache locator %q", source)
		}
	}
}
