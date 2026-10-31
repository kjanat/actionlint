package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/kjanat/go-yaml/v4"
)

func TestMigrationCanonicalSettings(t *testing.T) {
	original := []byte(`# repository settings
plugins: []
self-hosted-runner: {labels: [gpu, custom-*]}
config-variables: []
config-secrets: null
assume-default-permissions: permissive
paths: {"**": {ignore: ["legacy.*message"]}}
policy:
  cache-operation: false
  require-job-timeout: {min-minutes: 5, max-minutes: 30}
  required-actions: []
`)
	result, err := MigrateDocument("actionlint.yaml", original)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Notes) != 1 || !bytes.Contains(result.Content, []byte("# repository settings")) {
		t.Fatalf("migration: %+v\n%s", result, result.Content)
	}
	for _, key := range legacyKeys {
		if bytes.Contains(result.Content, []byte("\n"+key+":")) {
			t.Fatalf("legacy key remains: %s\n%s", key, result.Content)
		}
	}
	cfg, err := Parse(result.Content)
	if err != nil {
		t.Fatal(err)
	}
	if VariableNames(cfg.Facts()) == nil || len(VariableNames(cfg.Facts())) != 0 || SecretNames(cfg.Facts()) != nil {
		t.Fatal("unknown and empty name sets were conflated")
	}
	if !reflect.DeepEqual(RunnerLabels(cfg.Facts()), []string{"gpu", "custom-*"}) || configuredWorkflowPermissions(cfg) != "write" {
		t.Fatalf("knowledge lost: %+v", cfg)
	}
	if !cfg.Paths["**"].Ignore.Match("legacy some message") {
		t.Fatal("message suppression lost")
	}
	if ResolveRules(Presets{Strict: true}, cfg).CachePolicyEnabled("cache-operation") {
		t.Fatal("explicit migrated disable must survive strict preset")
	}
	second, err := MigrateDocument("actionlint.yaml", result.Content)
	if err != nil || second.Changed || !bytes.Equal(result.Content, second.Content) {
		t.Fatalf("not idempotent: %v", err)
	}
}

func TestMigrationPolicyNullRemainsReset(t *testing.T) {
	result, err := MigrateDocument("config.yaml", []byte("policy: {require-job-timeout: null}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Content, []byte("level: null")) {
		t.Fatalf("migration erased the explicit null: %s", result.Content)
	}
	cfg, err := Parse(result.Content)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequiresJobTimeout().Enabled() || DiagnosticLevel(cfg, "require-job-timeout") != "off" {
		t.Fatal("null did not restore the ordinary disabled default")
	}
	strict := ResolveRules(Presets{Strict: true}, cfg)
	if !strict.RequiresJobTimeout().Enabled() || DiagnosticLevel(strict, "require-job-timeout") != "on" {
		t.Fatal("null became an explicit disable instead of inheriting the strict preset")
	}
	second, err := MigrateDocument("config.yaml", result.Content)
	if err != nil || second.Changed || !bytes.Equal(result.Content, second.Content) {
		t.Fatalf("null migration not idempotent: %v", err)
	}
}

func TestMigrationConflicts(t *testing.T) {
	for _, test := range []struct{ source, target string }{
		{"policy: {cache-operation: false}\nlint: {rules: {correctness: {cache-operation: on}}}", "lint.rules.correctness.cache-operation"},
		{"config-variables: [OLD]\nlint: {rules: {correctness: {expression-reference: {level: on, options: {variables: [NEW]}}}}}", "repository.variables"},
		{"paths: {'**': {ignore: [old]}}\nlint: {message-ignores: {'**': [new]}}", "lint.message-ignores"},
	} {
		_, err := MigrateDocument("config.yaml", []byte(test.source))
		if err == nil || !strings.Contains(err.Error(), "conflicts with "+test.target) {
			t.Fatalf("missing actionable conflict: %v", err)
		}
	}
	for _, source := range []string{"self-hosted-runner: {lables: [gpu]}", "paths: {'**': {ignores: [typo]}}"} {
		if _, err := MigrateDocument("config.yaml", []byte(source)); err == nil || !strings.Contains(err.Error(), "without dropping") {
			t.Fatalf("unknown setting silently removed: %v", err)
		}
	}
}

func TestMigrationFormatsAndInheritance(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"actionlint.yaml", "config-variables: []\r\npolicy: {require-commit-hash: false}\r\n"},
		{"actionlint.json", `{"config-variables":[],"policy":{"require-commit-hash":false}}`},
		{"actionlint.toml", "config-variables = []\n[policy]\nrequire-commit-hash = false\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, tc.name)
			if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareMigration(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if string(before) != tc.source {
				t.Fatal("preparing migration wrote file")
			}
			if err := plan.Apply(t.Context()); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil || VariableNames(cfg.Facts()) == nil || cfg.RequiresCommitHash() {
				t.Fatalf("migration semantics: %+v %v", cfg, err)
			}
			if tc.name == "actionlint.yaml" && !bytes.Contains(plan.Content, []byte("\r\n")) {
				t.Fatal("CRLF changed")
			}
			entries, _ := os.ReadDir(directory)
			if len(entries) != 1 {
				t.Fatalf("temporary files leaked: %v", entries)
			}
		})
	}
}

func TestMigrationSnapshotRejectsConcurrentEdits(t *testing.T) {
	for _, changeBase := range []bool{false, true} {
		t.Run(strconv.FormatBool(changeBase), func(t *testing.T) {
			directory := t.TempDir()
			base, path := filepath.Join(directory, "base.yaml"), filepath.Join(directory, "actionlint.yaml")
			if err := os.WriteFile(base, []byte("config-variables: [BASE]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			source := []byte("extends: [base.yaml]\npolicy: {require-job-timeout: false}\n")
			if err := os.WriteFile(path, source, 0600); err != nil {
				t.Fatal(err)
			}
			plan, err := PrepareMigration(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			edited := path
			if changeBase {
				edited = base
			}
			if err := os.WriteFile(edited, []byte("config-variables: [USER_EDIT]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := plan.CheckUnchanged(t.Context()); err == nil || !strings.Contains(err.Error(), "changed before migration") {
				t.Fatalf("changed preparation snapshot accepted: %v", err)
			}
			if err := plan.Apply(t.Context()); err == nil || !strings.Contains(err.Error(), "changed before migration") {
				t.Fatalf("changed snapshot accepted: %v", err)
			}
			data, _ := os.ReadFile(edited)
			if !bytes.Contains(data, []byte("USER_EDIT")) {
				t.Fatal("user edit overwritten")
			}
		})
	}
}

func TestMigrationSnapshotFollowsSymlinkedInheritedInputs(t *testing.T) {
	directory := t.TempDir()
	selected := filepath.Join(directory, "actionlint.yaml")
	base := filepath.Join(directory, "base.yaml")
	other := filepath.Join(directory, "other.yaml")
	linkedBase := filepath.Join(directory, "linked-base.yaml")
	if err := os.WriteFile(base, []byte("config-variables: [BASE]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("config-variables: [OTHER]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, linkedBase); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(selected, []byte("extends: [linked-base.yaml]\npolicy: {require-job-timeout: false}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("unchanged target", func(t *testing.T) {
		plan, err := PrepareMigration(t.Context(), selected)
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.CheckUnchanged(t.Context()); err != nil {
			t.Fatalf("unchanged symlinked inherited input rejected: %v", err)
		}
		if err := plan.Apply(t.Context()); err != nil {
			t.Fatalf("migration with unchanged symlinked inherited input failed: %v", err)
		}
	})

	for _, tc := range []struct {
		name   string
		change func() error
	}{
		{name: "retargeted", change: func() error {
			if err := os.Remove(linkedBase); err != nil {
				return err
			}
			return os.Symlink(other, linkedBase)
		}},
		{name: "mutated target", change: func() error {
			return os.WriteFile(base, []byte("config-variables: [CHANGED]\n"), 0600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(selected, []byte("extends: [linked-base.yaml]\npolicy: {require-job-timeout: false}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(linkedBase); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(base, linkedBase); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			plan, err := PrepareMigration(t.Context(), selected)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.change(); err != nil {
				t.Fatal(err)
			}
			if err := plan.CheckUnchanged(t.Context()); err == nil || !strings.Contains(err.Error(), "changed before migration") {
				t.Fatalf("changed symlinked inherited input accepted: %v", err)
			}
		})
	}
}

func TestMigrationYAMLBooleanSpellings(t *testing.T) {
	for _, spelling := range []string{"true", "True", "TRUE"} {
		t.Run(spelling, func(t *testing.T) {
			migration, err := MigrateDocument("config.yaml", []byte("policy: {require-commit-hash: "+spelling+"}\n"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Parse(migration.Content)
			if err != nil {
				t.Fatal(err)
			}
			if DiagnosticLevel(cfg, "require-commit-hash") != "on" {
				t.Fatalf("boolean %s lost enabled state: %s", spelling, migration.Content)
			}
		})
	}
}

func TestMigrationAliasesRetainGlobalKnowledge(t *testing.T) {
	source := []byte(`overrides:
  - includes: [special.yml]
    lint:
      rules:
        correctness:
          runner-label:
            level: default
            options: &labels {labels: [gpu]}
self-hosted-runner: *labels
`)
	var direct Config
	if err := yaml.Unmarshal(source, &direct); err != nil {
		t.Fatal(err)
	}
	overlay, err := ParseOverlay("config", source)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _, err := Overlaid(nil, nil, []Overlay{overlay})
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []*Config{&direct, resolved} {
		if !reflect.DeepEqual(RunnerLabels(cfg.Facts()), []string{"gpu"}) {
			t.Fatalf("aliased global runner knowledge dropped: %+v", cfg.Facts())
		}
	}
}

func TestMigrationRetainsInheritedResetsAndEmptyMaps(t *testing.T) {
	directory := t.TempDir()
	base, path := filepath.Join(directory, "base.yaml"), filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(base, []byte(`config-variables: [BASE]
config-secrets: [TOKEN]
paths: {'**': {ignore: [legacy]}}
policy: {require-job-timeout: {max-minutes: 30}}
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`extends: [base.yaml]
config-variables: []
config-secrets: null
paths: {'**': {}}
policy: {require-job-timeout: false}
`), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if VariableNames(cfg.Facts()) == nil || len(VariableNames(cfg.Facts())) != 0 || SecretNames(cfg.Facts()) != nil || cfg.RequiresJobTimeout().Enabled() || !cfg.Paths["**"].Ignore.Match("legacy") {
			t.Fatalf("inheritance changed: %+v", cfg)
		}
		plan, err := PrepareMigration(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.Apply(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationRejectsLostInheritedTOMLReset(t *testing.T) {
	directory := t.TempDir()
	base := filepath.Join(directory, "base.toml")
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(base, []byte("[policy.require-job-timeout]\nmax-minutes = 30\n"), 0600); err != nil {
		t.Fatal(err)
	}
	original := []byte("extends = ['base.toml']\n[policy]\nrequire-job-timeout = true\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareMigration(t.Context(), path); err == nil || !strings.Contains(err.Error(), "cannot preserve inherited settings") {
		t.Fatalf("lost inherited reset accepted: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, original) {
		t.Fatalf("failed preparation changed config: %q, %v", content, err)
	}
}

func TestCanonicalKnowledgeRoundTripAndOverrides(t *testing.T) {
	source := []byte(`lint:
  rules:
    correctness:
      runner-label: {level: off, options: {labels: [gpu]}}
      expression-reference: {level: on, options: {variables: [], secrets: null}}
overrides:
  - includes: [release.yml]
    lint:
      rules:
        correctness:
          expression-reference: {level: on, options: {secrets: [RELEASE_TOKEN]}}
`)
	cfg, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := ForFile(cfg, "release.yml", "")
	if err != nil {
		t.Fatal(err)
	}
	if VariableNames(effective.Facts()) == nil || !reflect.DeepEqual(SecretNames(effective.Facts()), []string{"RELEASE_TOKEN"}) || !reflect.DeepEqual(RunnerLabels(effective.Facts()), []string{"gpu"}) {
		t.Fatalf("override knowledge: %+v", effective)
	}
}
