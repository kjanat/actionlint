package pluginref

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectURL(t *testing.T) {
	source := "https://EXAMPLE.com/Plugin.wasm?version=1#sha256=" + strings.Repeat("AB", 32)
	ref, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.com/Plugin.wasm?version=1#sha256=" + strings.Repeat("ab", 32)
	if ref.DirectSource() != want || ref.String() != want || ref.Registry != "" || ref.Version != "" {
		t.Fatalf("unexpected direct declaration: %+v", ref)
	}
	if normalized, err := ref.Normalize(); err != nil || !ref.Equal(normalized) {
		t.Fatalf("roundtrip: %+v %v", normalized, err)
	}
	for _, invalid := range []string{
		"https://example.com/plugin.wasm", "https://example.com/plugin.wasm#sha256=bad",
		"http://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32),
		"https://user:pass@example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32),
	} {
		if _, err := Parse(invalid); err == nil {
			t.Errorf("accepted %s", invalid)
		}
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		input, name, version, canonical string
	}{
		{"ruff", "ruff", "latest", "ruff"},
		{"shellcheck@v1.2.3", "shellcheck", "v1.2.3", "shellcheck@v1.2.3"},
		{"github.com/kjanat/actionlint-plugin-ruff@v1.2.3", "github.com/kjanat/actionlint-plugin-ruff", "v1.2.3", "github.com/kjanat/actionlint-plugin-ruff@v1.2.3"},
		{"github.com/other/actionlint-plugin-ruff", "github.com/other/actionlint-plugin-ruff", "latest", "github.com/other/actionlint-plugin-ruff"},
		{"github.com/owner/.github", "github.com/owner/.github", "latest", "github.com/owner/.github"},
		{"Kjanat/Shellcheck", "github.com/kjanat/shellcheck", "latest", "github.com/kjanat/shellcheck"},
		{"github.com/Kjanat/Shellcheck@v1.2.3", "github.com/kjanat/shellcheck", "v1.2.3", "github.com/kjanat/shellcheck@v1.2.3"},
		{"gitlab.com/Group/Subgroup/Plugin@latest", "gitlab.com/group/subgroup/plugin", "latest", "gitlab.com/group/subgroup/plugin"},
		{"gitlab.com/Backend.Services/Team.Tools/Plugin@v1", "gitlab.com/backend.services/team.tools/plugin", "v1", "gitlab.com/backend.services/team.tools/plugin@v1"},
		{"gitlab.com/_group/.sub_group/plugin.", "gitlab.com/_group/.sub_group/plugin.", "latest", "gitlab.com/_group/.sub_group/plugin."},
		{"gitlab.com/group..name/subgroup_/plugin--name", "gitlab.com/group..name/subgroup_/plugin--name", "latest", "gitlab.com/group..name/subgroup_/plugin--name"},
		{"gitlab.com/group.-name/plugin", "gitlab.com/group.-name/plugin", "latest", "gitlab.com/group.-name/plugin"},
		{"owner/repo@Release-1+build", "github.com/owner/repo", "Release-1+build", "github.com/owner/repo@Release-1+build"},
		{"owner/repo@area/v1.2.3", "github.com/owner/repo", "area/v1.2.3", "github.com/owner/repo@area/v1.2.3"},
		{"owner/repo@area/v1.LOCK", "github.com/owner/repo", "area/v1.LOCK", "github.com/owner/repo@area/v1.LOCK"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			ref, err := Parse(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if ref.Name != tc.name || ref.Version != tc.version || ref.Registry != DefaultRegistry || ref.String() != tc.canonical {
				t.Fatalf("unexpected reference: %#v (%s)", ref, ref.String())
			}
		})
	}
}

func TestRejectInvalidReference(t *testing.T) {
	for _, input := range []string{
		"", ".plugin", "under_score", strings.Repeat("a", 65), "owner/repo@", "owner/repo@@v1", "owner/repo@^1.0", "owner/repo@v1//extra", "owner/repo@v1/../extra",
		"github.com/owner/repo/extra", "github.com/owner/.", "github.com/owner/..", "https://github.com/owner/repo", "codeberg.org/owner/repo",
		"owner/repo@area/..", "owner/repo@area/v1.lock", "owner/repo@area/v1.", "owner/repo@area//v1",
		"../repo", "owner/../repo", " owner/repo", "owner/repo ", "gitlab.com/group//repo",
		"github.com/backend.services/plugin", "gitlab.com/-group/plugin", "gitlab.com/group./plugin",
		"gitlab.com/group/subgroup./plugin", "gitlab.com/group/-plugin", "gitlab.com/group/plugin.git",
		"gitlab.com/group/plugin.atom", "gitlab.com/group/../plugin", "gitlab.com/group%2ename/plugin",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}

func TestRegistry(t *testing.T) {
	ref, err := New("owner/repo", "v1", "https://Example.com/catalog")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Registry != "https://example.com/catalog/" {
		t.Fatalf("unexpected registry: %s", ref.Registry)
	}
	directory := t.TempDir()
	local := url.URL{Scheme: "file", Path: filepath.ToSlash(directory)}
	if !strings.HasPrefix(local.Path, "/") {
		local.Path = "/" + local.Path
	}
	ref, err = New("owner/repo", "v1", local.String())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := LocalRegistryDirectory(ref.Registry); err != nil || got != directory {
		t.Fatalf("local catalogue: %s, %v", got, err)
	}
	for _, registry := range []string{
		"http://example.com/", "https://", "https://:443/", "https://user:secret@example.com/",
		"file://server/share", "file:////server/share", "file:relative", "file:///catalog?x=1", "file:///catalog#fragment",
		"https://example.com/?x=1", "https://example.com/?", "https://example.com/#hash",
		"https://example.com/#", "https://example.com/a%5Cb",
	} {
		if _, err := New("owner/repo", "", registry); err == nil {
			t.Errorf("accepted registry %q", registry)
		}
	}
}

func TestRegistryPreservesIPv6Zone(t *testing.T) {
	for _, source := range []string{
		"https://[FE80::1%25Ethernet]/",
		"https://[FE80::1%25Ethernet]:443/catalog/",
	} {
		ref, err := New("owner/repo", "v1", source)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(source, "FE80", "fe80", 1)
		if ref.Registry != want {
			t.Fatalf("registry=%q, want %q", ref.Registry, want)
		}
	}
}

// Equivalent directory spellings must produce one manifest and lock identity,
// including paths whose names contain URL delimiters.
func TestLocalRegistryNormalization(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "space # percent%", "catalogue")
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(directory)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	want := u.String() + "/"
	for _, suffix := range []string{"", "/", "/./", "/discard/../", "/%2e/"} {
		ref, err := New("Owner/Plugin", "v1", u.String()+suffix)
		if err != nil {
			t.Fatal(err)
		}
		if ref.Registry != want {
			t.Fatalf("registry %q normalized to %q, want %q", suffix, ref.Registry, want)
		}
		got, err := LocalRegistryDirectory(ref.Registry)
		if err != nil || got != directory {
			t.Fatalf("directory changed: %q, %v", got, err)
		}
		normalized, err := ref.Normalize()
		if err != nil || !normalized.Equal(ref) {
			t.Fatalf("local reference changed on roundtrip: %+v, %v", normalized, err)
		}
	}
}

// Only local absolute directory URIs may opt into candidate installation.
func TestLocalRegistryRejectsAuthorityAndAmbiguousPaths(t *testing.T) {
	for _, value := range []string{
		"", "catalogue", "file:", "file://", "file:catalogue", "file://catalogue", "file://localhost/catalogue",
		"file://user:password@server/catalogue", "file:////server/share", "file:///%2fserver/share",
		"file:///catalogue?", "file:///catalogue?version=1", "file:///catalogue#", "file:///catalogue#part",
		"file:///catalogue%5cchild", "file:///catalogue%zz", "https://example.com/catalogue/",
	} {
		t.Run(value, func(t *testing.T) {
			if directory, err := LocalRegistryDirectory(value); err == nil {
				t.Fatalf("accepted %q as local directory %q", value, directory)
			}
		})
	}
}
