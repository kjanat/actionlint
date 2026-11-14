package config_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

func TestCategoryMigrationPreservesSelection(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`lint: {rules: {style: warn}}`,
		`lint: {rules: {style: {preset: all, empty-job: off}}}`,
		`lint: {rules: {style: {level: error, empty-run: default}}}`,
		`lint: {rules: {policy: {preset: none, require-job-timeout: {level: error, options: {max-minutes: 12}}}}}`,
		`lint: {rules: {external: off}}`,
		`lint: {rules: {external: {level: error, shellcheck: warn}}}`,
		`lint: {rules: {correctness: {uncancellable-condition: warn}}}`,
		`lint: {rules: {suspicious: {suspicious-coercion: warn}}}`,
		`lint: {rules: {disable: [suspicious-coercion]}}`,
		`lint: {rules: {policy: {require-commit-hash: off}}, enabled: true}`,
		`lint: {rules: {disable: [require-commit-hash]}}`,
		`lint: {rules: {conventions: {job-timeout: {level: error, options: {max-minutes: 20}}}}}`,
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			before, err := checking.ConfigurationRules().Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			migration, err := checking.ConfigurationRules().MigrateDocument("config.yaml", []byte(input))
			if err != nil {
				t.Fatal(err)
			}
			after, err := checking.ConfigurationRules().Parse(migration.Content)
			if err != nil {
				t.Fatal(err)
			}
			for _, preset := range []repoconfig.Presets{{}, {Strict: true}, {Experimental: new(true)}, {Experimental: new(false)}} {
				a, err := checking.ConfigurationRules().ResolveRules(preset, before)
				if err != nil {
					t.Fatal(err)
				}
				b, err := checking.ConfigurationRules().ResolveRules(preset, after)
				if err != nil {
					t.Fatal(err)
				}
				for _, descriptor := range checking.ConfigurationRules().Rules() {
					if checking.ConfigurationRules().DiagnosticLevel(a, descriptor.Name) != checking.ConfigurationRules().DiagnosticLevel(b, descriptor.Name) {
						t.Errorf("%s preset %+v: %s became %s\n%s", descriptor.Name, preset, checking.ConfigurationRules().DiagnosticLevel(a, descriptor.Name), checking.ConfigurationRules().DiagnosticLevel(b, descriptor.Name), migration.Content)
					}
				}
				if !reflect.DeepEqual(a.Policy, b.Policy) {
					t.Errorf("options changed: %#v -> %#v", a.Policy, b.Policy)
				}
			}
			second, err := checking.ConfigurationRules().MigrateDocument("config.yaml", migration.Content)
			if err != nil || second.Changed || !bytes.Equal(second.Content, migration.Content) {
				t.Fatalf("migration not idempotent: %v\n%s", err, second.Content)
			}
		})
	}
}

func TestCategoryMigrationPluginMetadata(t *testing.T) {
	t.Parallel()
	input := []byte(`lint: {rules: {plugins: {level: off, custom-check: {level: warn, options: {mode: audit}}}}}`)
	descriptor := rule.Descriptor{Name: "custom-check", Category: "security", Maturity: "stable", OptionsSchema: json.RawMessage(`{"type":"object","properties":{"mode":{"enum":["audit"]}},"additionalProperties":false}`)}
	if _, err := checking.ConfigurationRules().MigrateDocument("config.yaml", input); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("missing metadata must not guess a category: %v", err)
	}
	result, err := checking.ConfigurationRules().MigrateDocument("config.yaml", input, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	before, err := checking.ConfigurationRules().Parse(input, descriptor.Name)
	if err != nil {
		t.Fatal(err)
	}
	after, err := checking.ConfigurationRules().Parse(result.Content, descriptor.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, preset := range []repoconfig.Presets{{}, {Strict: true}} {
		a, err := checking.ConfigurationRules().ResolveRules(preset, before, descriptor)
		if err != nil {
			t.Fatal(err)
		}
		b, err := checking.ConfigurationRules().ResolveRules(preset, after, descriptor)
		if err != nil {
			t.Fatal(err)
		}
		if checking.ConfigurationRules().DiagnosticLevel(a, descriptor.Name) != checking.ConfigurationRules().DiagnosticLevel(b, descriptor.Name) {
			t.Fatal("plugin severity changed")
		}
		old, err := a.Lint.Rules.RuleOptions(descriptor.Name)
		if err != nil {
			t.Fatal(err)
		}
		current, err := b.Lint.Rules.RuleOptions(descriptor.Name)
		if err != nil || !reflect.DeepEqual(old, current) {
			t.Fatalf("plugin options changed: %v", err)
		}
	}
}

func TestCategoryMigrationFormatsAndConflicts(t *testing.T) {
	t.Parallel()
	for filename, input := range map[string]string{
		"config.yaml": `lint: {rules: {policy: {require-commit-hash: error}}}`,
		"config.json": `{"lint":{"rules":{"policy":{"require-commit-hash":"error"}}}}`,
		"config.toml": "#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json\n[lint.rules.policy]\nrequire-commit-hash = 'error'\n",
	} {
		result, err := checking.ConfigurationRules().MigrateDocument(filename, []byte(input))
		if err != nil || !result.Changed || !bytes.Contains(result.Content, []byte("unpinned-reference")) || bytes.Contains(result.Content, []byte("require-commit-hash")) {
			t.Fatalf("%s: %v\n%s", filename, err, result.Content)
		}
		second, err := checking.ConfigurationRules().MigrateDocument(filename, result.Content)
		if err != nil || second.Changed {
			t.Fatalf("%s second migration: %v", filename, err)
		}
	}
	for filename, input := range map[string]string{
		"coercion.yaml": `lint: {rules: {suspicious: {suspicious-coercion: warn}}}`,
		"coercion.json": `{"lint":{"rules":{"suspicious":{"suspicious-coercion":"warn"}}}}`,
		"coercion.toml": "[lint.rules.suspicious]\nsuspicious-coercion = 'warn'\n",
	} {
		result, err := checking.ConfigurationRules().MigrateDocument(filename, []byte(input))
		if err != nil || !result.Changed || !bytes.Contains(result.Content, []byte("coercion")) || bytes.Contains(result.Content, []byte("suspicious-coercion")) {
			t.Fatalf("%s: %v\n%s", filename, err, result.Content)
		}
		second, err := checking.ConfigurationRules().MigrateDocument(filename, result.Content)
		if err != nil || second.Changed || !bytes.Equal(second.Content, result.Content) {
			t.Fatalf("%s second migration: %v", filename, err)
		}
	}
	for _, input := range []string{
		`lint: {rules: {policy: {require-commit-hash: off}, security: {unpinned-reference: error}}}`,
		`lint: {rules: {security: {require-commit-hash: off, unpinned-reference: error}}}`,
		`lint: {rules: {suspicious: {suspicious-coercion: off, coercion: error}}}`,
	} {
		if _, err := checking.ConfigurationRules().MigrateDocument("config.yaml", []byte(input)); err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("conflict silently accepted: %v", err)
		}
	}
}

func TestCategoryFileMigrationChecksInheritedSelection(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	base, leaf := filepath.Join(directory, "base.yaml"), filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(base, []byte("lint: {rules: {style: {level: warn}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("extends: [base.yaml]\nlint: {rules: {style: {empty-job: off}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := checking.ConfigurationRules().PrepareMigration(t.Context(), leaf, acquisitionhttp.Connections{})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := document.Parse(leaf, prepared.Content)
	if err != nil {
		t.Fatal(err)
	}
	root := parsed.Value.(map[string]any)
	lint := root["lint"].(map[string]any)
	rules := lint["rules"].(map[string]any)
	group, ok := rules["correctness"].(map[string]any)
	if !ok || group["empty-job"] != "off" {
		t.Fatalf("wrong migration: %s", prepared.Content)
	}
}
