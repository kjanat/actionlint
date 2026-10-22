package config

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/pluginref"
	"github.com/google/go-cmp/cmp"
	"go.yaml.in/yaml/v4"
)

func TestPluginDeclarations(t *testing.T) {
	cfg, err := Parse([]byte(`plugins:
  - Kjanat/Shellcheck@v1
  - name: gitlab.com/Group/Subgroup/Plugin
    version: latest
    registry: https://Example.com/catalog
`))
	if err != nil {
		t.Fatal(err)
	}
	want := PluginDeclarations{
		{Name: "github.com/kjanat/shellcheck", Version: "v1", Registry: pluginref.DefaultRegistry},
		{Name: "gitlab.com/group/subgroup/plugin", Version: "latest", Registry: "https://example.com/catalog/"},
	}
	if diff := cmp.Diff(want, cfg.Plugins); diff != "" {
		t.Fatal(diff)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, roundTrip.Plugins); diff != "" {
		t.Fatal(diff)
	}
}

func TestPluginDeclarationsPresence(t *testing.T) {
	for _, input := range []string{"plugins: null", "plugins: []", "plugins:"} {
		cfg, err := Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Plugins == nil || len(cfg.Plugins) != 0 {
			t.Fatalf("%q must explicitly select zero plugins", input)
		}
	}
	cfg, err := Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins != nil {
		t.Fatal("omission must remain distinct from an empty selection")
	}
}

func TestPluginDeclarationsReject(t *testing.T) {
	for _, input := range []string{
		"plugins: owner/repo", "plugins: [null]", "plugins: [42]", "plugins: [{}]",
		"plugins: [{name: owner/repo, extra: true}]", "plugins: [{name: owner/repo, version: 1}]",
		"plugins: [{name: owner/repo, version: null}]", "plugins: [{name: owner/repo, registry: ''}]",
		"plugins: [{name: owner/repo, version: ''}]", "plugins: [{name: owner/repo, name: other/repo}]",
		"plugins: [owner/repo, github.com/OWNER/repo@v2]",
		"plugins: [owner/repo, {name: owner/repo, registry: 'https://custom.example/'}]",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}

func TestPluginDeclarationsExtends(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("base.yaml", "plugins: [owner/first@v1, owner/second]")
	for _, tc := range []struct {
		suffix string
		count  int
		name   string
	}{
		{"", 2, "github.com/owner/first"},
		{"plugins: [owner/replacement]", 1, "github.com/owner/replacement"},
		{"plugins: null", 0, ""},
		{"plugins: []", 0, ""},
	} {
		path := write("leaf.yaml", "extends: [base.yaml]\n"+tc.suffix)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Plugins == nil || len(cfg.Plugins) != tc.count {
			t.Fatalf("%q produced %#v", tc.suffix, cfg.Plugins)
		}
		if tc.count > 0 && cfg.Plugins[0].Name != tc.name {
			t.Fatalf("unexpected inherited list: %#v", cfg.Plugins)
		}
	}
}

func TestSelectedPathAllFormats(t *testing.T) {
	for _, name := range RepositoryConfigNames {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(dir, ".github"), 0o700); err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(dir, ".github", name)
			if err := os.WriteFile(want, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			got, err := SelectedPath(Selection{})
			if err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		})
	}
}
