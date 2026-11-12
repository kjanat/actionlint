package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/tree"
)

func writeNeutralFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNeutralMixedFormatExtends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeNeutralFixture(t, dir, "base.toml", "[repository]\nrunners = ['gpu']\nsecrets = ['BASE']\n")
	writeNeutralFixture(t, dir, "middle.jsonc", "{\n // inherit TOML\n \"extends\": [\"base.toml\"],\n \"repository\": {\"secrets\": [],},\n}\n")
	leaf := writeNeutralFixture(t, dir, "leaf.yaml", "extends: [middle.jsonc]\nrepository:\n  runners: []\n")
	loaded, err := tree.Load(leaf, os.ReadFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"repository": map[string]any{"runners": []any{}, "secrets": []any{}}}
	if !reflect.DeepEqual(loaded.Value, want) {
		t.Fatalf("got %#v, want %#v", loaded.Value, want)
	}
	if len(loaded.Files) != 3 {
		t.Fatalf("sources: %v", loaded.Files)
	}
	if _, err := checking.ConfigurationRules().Load(leaf); err != nil {
		t.Fatalf("load mixed-format graph: %v", err)
	}
}

func TestNeutralNullOmissionAndEmptyCollections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, overlay string
		want          any
	}{
		{"omitted", "", map[string]any{"mapping": map[string]any{"one": "base"}, "list": []any{"base"}}},
		{"null", "mapping: null", map[string]any{"mapping": nil, "list": []any{"base"}}},
		{"empty mapping", "mapping: {}", map[string]any{"mapping": map[string]any{"one": "base"}, "list": []any{"base"}}},
		{"empty list", "list: []", map[string]any{"mapping": map[string]any{"one": "base"}, "list": []any{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeNeutralFixture(t, dir, "base.yaml", "mapping: {one: base}\nlist: [base]\n")
			leaf := writeNeutralFixture(t, dir, "leaf.yaml", "extends: [base.yaml]\n"+tc.overlay+"\n")
			got, err := tree.Load(leaf, os.ReadFile, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Value, tc.want) {
				t.Fatalf("got %#v, want %#v", got.Value, tc.want)
			}
		})
	}
}

func TestNeutralFileOverrideAfterNull(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {correctness: off}}\noverrides:\n  - includes: ['**']\n    lint: {rules: {correctness: null}}\n  - includes: ['**']\n    lint: {rules: {correctness: {if-cond: warn}}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := checking.ConfigurationRules().ForFile(cfg, "ci.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := checking.ConfigurationRules().DiagnosticLevel(selected, "if-cond"); got != "warn" {
		t.Fatalf("if-cond=%s", got)
	}
	if got := checking.ConfigurationRules().DiagnosticLevel(selected, "expression-reference"); got != "on" {
		t.Fatalf("expression-reference=%s", got)
	}
}

func TestNeutralLegacyPermissionScopeMigration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, source string }{
		{"policy", "policy: {require-permissions: {scope: job}}"},
		{"rule", "lint: {rules: {security: {explicit-permissions: {level: on, options: {scope: job}}}}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			migration, err := checking.ConfigurationRules().MigrateDocument("legacy.yaml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := checking.ConfigurationRules().Parse(migration.Content)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := checking.ConfigurationRules().ResolveRules(config.Presets{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			locations, err := resolved.PermissionDeclarationLocations()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(locations, []config.PermissionLocation{config.EveryJobPermissions}) {
				t.Fatalf("locations=%v; migrated document=%s", locations, migration.Content)
			}
		})
	}
}
