package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/rule"
)

func TestCategoryMigrationPreservesSelection(t *testing.T) {
	for _, input := range []string{
		`lint: {rules: {style: warn}}`,
		`lint: {rules: {style: {preset: all, empty-job: off}}}`,
		`lint: {rules: {style: {level: error, empty-run: default}}}`,
		`lint: {rules: {policy: {preset: none, require-job-timeout: {level: error, options: {max-minutes: 12}}}}}`,
		`lint: {rules: {external: off}}`,
		`lint: {rules: {external: {level: error, shellcheck: warn}}}`,
		`lint: {rules: {correctness: {uncancellable-condition: warn}}}`,
		`lint: {rules: {policy: {require-commit-hash: off}}, enabled: true}`,
		`lint: {rules: {disable: [require-commit-hash]}}`,
		`lint: {rules: {conventions: {job-timeout: {level: error, options: {max-minutes: 20}}}}}`,
	} {
		t.Run(input, func(t *testing.T) {
			before, err := Parse([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			migration, err := MigrateDocument("config.yaml", []byte(input))
			if err != nil {
				t.Fatal(err)
			}
			after, err := Parse(migration.Content)
			if err != nil {
				t.Fatal(err)
			}
			for _, preset := range []Presets{{}, {Strict: true}, {Experimental: new(true)}, {Experimental: new(false)}} {
				a, err := ResolveRules(preset, before)
				if err != nil {
					t.Fatal(err)
				}
				b, err := ResolveRules(preset, after)
				if err != nil {
					t.Fatal(err)
				}
				for _, descriptor := range Rules() {
					if DiagnosticLevel(a, descriptor.Name) != DiagnosticLevel(b, descriptor.Name) {
						t.Errorf("%s preset %+v: %s became %s\n%s", descriptor.Name, preset, DiagnosticLevel(a, descriptor.Name), DiagnosticLevel(b, descriptor.Name), migration.Content)
					}
				}
				if !reflect.DeepEqual(a.Policy, b.Policy) {
					t.Errorf("options changed: %#v -> %#v", a.Policy, b.Policy)
				}
			}
			second, err := MigrateDocument("config.yaml", migration.Content)
			if err != nil || second.Changed || !bytes.Equal(second.Content, migration.Content) {
				t.Fatalf("migration not idempotent: %v\n%s", err, second.Content)
			}
		})
	}
}

func TestCategoryMigrationPluginMetadata(t *testing.T) {
	input := []byte(`lint: {rules: {plugins: {level: off, custom-check: {level: warn, options: {mode: audit}}}}}`)
	descriptor := rule.Descriptor{Name: "custom-check", Category: "security", Maturity: "stable", OptionsSchema: json.RawMessage(`{"type":"object","properties":{"mode":{"enum":["audit"]}},"additionalProperties":false}`)}
	if _, err := MigrateDocument("config.yaml", input); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("missing metadata must not guess a category: %v", err)
	}
	result, err := MigrateDocument("config.yaml", input, descriptor)
	if err != nil {
		t.Fatal(err)
	}
	before, err := Parse(input, descriptor.Name)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse(result.Content, descriptor.Name)
	if err != nil {
		t.Fatal(err)
	}
	for _, preset := range []Presets{{}, {Strict: true}} {
		a, err := ResolveRules(preset, before, descriptor)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ResolveRules(preset, after, descriptor)
		if err != nil {
			t.Fatal(err)
		}
		if DiagnosticLevel(a, descriptor.Name) != DiagnosticLevel(b, descriptor.Name) {
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
	for filename, input := range map[string]string{
		"config.yaml": `lint: {rules: {policy: {require-commit-hash: error}}}`,
		"config.json": `{"lint":{"rules":{"policy":{"require-commit-hash":"error"}}}}`,
		"config.toml": "#:schema https://cdn.jsdelivr.net/gh/kjanat/actionlint@2/actionlint.schema.json\n[lint.rules.policy]\nrequire-commit-hash = 'error'\n",
	} {
		result, err := MigrateDocument(filename, []byte(input))
		if err != nil || !result.Changed || !bytes.Contains(result.Content, []byte("unpinned-reference")) || bytes.Contains(result.Content, []byte("require-commit-hash")) {
			t.Fatalf("%s: %v\n%s", filename, err, result.Content)
		}
		second, err := MigrateDocument(filename, result.Content)
		if err != nil || second.Changed {
			t.Fatalf("%s second migration: %v", filename, err)
		}
	}
	for _, input := range []string{
		`lint: {rules: {policy: {require-commit-hash: off}, security: {unpinned-reference: error}}}`,
		`lint: {rules: {security: {require-commit-hash: off, unpinned-reference: error}}}`,
	} {
		if _, err := MigrateDocument("config.yaml", []byte(input)); err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("conflict silently accepted: %v", err)
		}
	}
}

func TestCategoryFileMigrationChecksInheritedSelection(t *testing.T) {
	directory := t.TempDir()
	base, leaf := filepath.Join(directory, "base.yaml"), filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(base, []byte("lint: {rules: {style: {level: warn}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("extends: [base.yaml]\nlint: {rules: {style: {empty-job: off}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareMigration(t.Context(), leaf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(prepared.Content, []byte("suspicious:")) || !bytes.Contains(prepared.Content, []byte("empty-job: off")) {
		t.Fatalf("wrong migration: %s", prepared.Content)
	}
}
