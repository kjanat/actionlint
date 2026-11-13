package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
	"github.com/google/go-cmp/cmp"
)

func TestDirectPluginDeclaration(t *testing.T) {
	t.Parallel()
	source := "https://example.com/plugin.wasm#sha256=" + strings.Repeat("ab", 32)
	cfg, err := checking.ConfigurationRules().Parse([]byte("plugins: [\"" + source + "\"]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].DirectSource() != source {
		t.Fatalf("unexpected declarations: %+v", cfg.Plugins)
	}
	data, err := encodeConfigYAML(cfg.Plugins)
	if err != nil {
		t.Fatal(err)
	}
	var refs repoconfig.PluginDeclarations
	if err := decodeConfigYAML(data, &refs); err != nil || !refs[0].Equal(cfg.Plugins[0]) {
		t.Fatalf("roundtrip: %s %v", data, err)
	}
	duplicate := "plugins: [\"" + source + "\", \"" + strings.ReplaceAll(source, "abab", "cdcd") + "\"]"
	if _, err := checking.ConfigurationRules().Parse([]byte(duplicate)); err == nil {
		t.Fatal("accepted two pins for the same URL")
	}
}

func TestPluginDeclarations(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte(`plugins:
  - Kjanat/Shellcheck@v1
  - name: gitlab.com/Group/Subgroup/Plugin
    version: latest
    registry: https://Example.com/catalog
`))
	if err != nil {
		t.Fatal(err)
	}
	want := [][3]string{
		{"github.com/kjanat/shellcheck", "v1", pluginref.DefaultRegistry},
		{"gitlab.com/group/subgroup/plugin", "latest", "https://example.com/catalog/"},
	}
	if diff := cmp.Diff(want, declarationFields(cfg.Plugins)); diff != "" {
		t.Fatal(diff)
	}
	data, err := checking.ConfigurationRules().Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := checking.ConfigurationRules().Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, declarationFields(roundTrip.Plugins)); diff != "" {
		t.Fatal(diff)
	}
}

// declarationFields lists the name, version and registry of each declaration.
func declarationFields(refs repoconfig.PluginDeclarations) [][3]string {
	fields := make([][3]string, 0, len(refs))
	for _, ref := range refs {
		fields = append(fields, [3]string{ref.Name(), ref.Version(), ref.Registry()})
	}
	return fields
}

func TestPluginAliasRegistryScope(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte(`plugins:
  - community-check
  - name: community-check
    registry: https://custom.example/
  - github.com/kjanat/actionlint-plugin-ruff
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 3 || cfg.Plugins[0].Name() != "community-check" || cfg.Plugins[1].Name() != "community-check" ||
		cfg.Plugins[2].String() != "github.com/kjanat/actionlint-plugin-ruff" {
		t.Fatalf("alias scope or explicit repository rewritten: %+v", cfg.Plugins)
	}
	data, err := encodeConfigYAML(cfg.Plugins)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip repoconfig.PluginDeclarations
	if err := decodeConfigYAML(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(cfg.Plugins, roundTrip); diff != "" {
		t.Fatal(diff)
	}
}

func TestPluginDeclarationsPresence(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"plugins: null", "plugins: []", "plugins:"} {
		cfg, err := checking.ConfigurationRules().Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Plugins == nil || len(cfg.Plugins) != 0 {
			t.Fatalf("%q must explicitly select zero plugins", input)
		}
	}
	cfg, err := checking.ConfigurationRules().Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins != nil {
		t.Fatal("omission must remain distinct from an empty selection")
	}
}

func TestPluginDeclarationsReject(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"plugins: owner/repo", "plugins: [null]", "plugins: [42]", "plugins: [{}]",
		"plugins: [{name: owner/repo, extra: true}]", "plugins: [{name: owner/repo, version: 1}]",
		"plugins: [{name: owner/repo, version: null}]", "plugins: [{name: owner/repo, registry: ''}]",
		"plugins: [{name: owner/repo, version: ''}]", "plugins: [{name: owner/repo, name: other/repo}]",
		"plugins: [owner/repo, github.com/OWNER/repo@v2]",
		"plugins: [owner/repo, {name: owner/repo, registry: 'https://custom.example/'}]",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := checking.ConfigurationRules().Parse([]byte(input)); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}

func TestPluginDeclarationsExtends(t *testing.T) {
	t.Parallel()
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
		cfg, err := checking.ConfigurationRules().Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Plugins == nil || len(cfg.Plugins) != tc.count {
			t.Fatalf("%q produced %#v", tc.suffix, cfg.Plugins)
		}
		if tc.count > 0 && cfg.Plugins[0].Name() != tc.name {
			t.Fatalf("unexpected inherited list: %#v", cfg.Plugins)
		}
	}
}

func TestSelectedPathAllFormats(t *testing.T) {
	for _, name := range repoconfig.RepositoryConfigNames {
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
			got, err := repoconfig.SelectedPath(repoconfig.Selection{})
			if err != nil || got != want {
				t.Fatalf("got %q, %v; want %q", got, err, want)
			}
		})
	}
}
