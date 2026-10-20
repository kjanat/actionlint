package pluginref

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		input, name, version, canonical string
	}{
		{"Kjanat/Shellcheck", "github.com/kjanat/shellcheck", "latest", "github.com/kjanat/shellcheck"},
		{"github.com/Kjanat/Shellcheck@v1.2.3", "github.com/kjanat/shellcheck", "v1.2.3", "github.com/kjanat/shellcheck@v1.2.3"},
		{"gitlab.com/Group/Subgroup/Plugin@latest", "gitlab.com/group/subgroup/plugin", "latest", "gitlab.com/group/subgroup/plugin"},
		{"owner/repo@Release-1+build", "github.com/owner/repo", "Release-1+build", "github.com/owner/repo@Release-1+build"},
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
		"", "plugin", "owner/repo@", "owner/repo@@v1", "owner/repo@^1.0", "owner/repo@v1/extra",
		"github.com/owner/repo/extra", "https://github.com/owner/repo", "codeberg.org/owner/repo",
		"../repo", "owner/../repo", " owner/repo", "owner/repo ", "gitlab.com/group//repo",
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
	for _, registry := range []string{
		"http://example.com/", "file:///catalog", "https://", "https://:443/", "https://user:secret@example.com/",
		"https://example.com/?x=1", "https://example.com/?", "https://example.com/#hash",
		"https://example.com/#", "https://example.com/a%5Cb",
	} {
		if _, err := New("owner/repo", "", registry); err == nil {
			t.Errorf("accepted registry %q", registry)
		}
	}
}
