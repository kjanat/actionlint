package configsource

import (
	"path/filepath"
	"testing"
)

func TestNormalizeLocalPercentFilename(t *testing.T) {
	t.Parallel()
	name := filepath.Join(t.TempDir(), "100%preset.yaml")
	got, err := NormalizeAddress(name)
	if err != nil || got != name {
		t.Fatalf("local source = %q, %v", got, err)
	}
}

func TestNormalizeHTTPSIdentity(t *testing.T) {
	t.Parallel()
	got, err := NormalizeAddress("HTTPS://EXAMPLE.TEST/base.toml?version=1")
	if err != nil || got != "https://example.test/base.toml?version=1" {
		t.Fatalf("HTTPS identity = %q, %v", got, err)
	}
}

func TestNormalizeHTTPSCanonicalAuthority(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"https://example.test:443/base.toml":   "https://example.test/base.toml",
		"https://example.test:0443/base.toml":  "https://example.test/base.toml",
		"https://BÜCHER.example/base.toml":     "https://xn--bcher-kva.example/base.toml",
		"https://[0:0::1]:8443/base.toml?v=1":  "https://[::1]:8443/base.toml?v=1",
		"https://example.test:08443/base.toml": "https://example.test:8443/base.toml",
	} {
		if got, err := NormalizeAddress(name); err != nil || got != want {
			t.Errorf("NormalizeAddress(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	if got, err := NormalizeAddress("https://under_score.example/base.toml"); err == nil {
		t.Errorf("accepted undialable host as %q", got)
	}
}

func TestRejectInvalidSchemeAndEmptyFragment(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"https://example.test/a.yaml#", "https://:443/a.yaml", "http:/example.test/a.yaml", "file:/tmp/a.yaml"} {
		if got, err := ResolveAddress(filepath.Join(t.TempDir(), "config.yaml"), name); err == nil {
			t.Errorf("accepted %q as %q", name, got)
		}
	}
}

func TestRemoteRelativeFragment(t *testing.T) {
	t.Parallel()
	if got, err := ResolveAddress("https://example.test/base.yaml", "child.yaml#"); err == nil {
		t.Fatalf("accepted fragment as %q", got)
	}
}
