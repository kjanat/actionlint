package config

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	configtree "actionlint.kjanat.dev/v2/internal/config/tree"
	"github.com/pelletier/go-toml/v2"
)

func TestTOMLMigrationPreservesComments(t *testing.T) {
	original := []byte("#:schema https://actionlint.kjanat.dev/schema/config.json\r\n" +
		"# settings for all workflows\r\n" +
		"plugins = [] # unchanged root statement\r\n" +
		"config-variables = [\r\n" +
		"  # variable comment\r\n" +
		"  \"NAME#VALUE\", # array item comment\r\n" +
		"] # legacy value comment\r\n" +
		"[policy] # legacy table comment\r\n" +
		"# policy setting comment\r\n" +
		"require-commit-hash = false # policy inline comment\r\n" +
		"[lint.rules.correctness]\r\n" +
		"# canonical setting comment\r\n" +
		"cache-operation = { level = \"on\" } # canonical inline comment\r\n")

	migration, err := MigrateDocument("actionlint.toml", original)
	if err != nil {
		t.Fatal(err)
	}
	if !migration.Changed {
		t.Fatal("migration did not change legacy TOML")
	}
	for _, comment := range []string{
		"#:schema https://actionlint.kjanat.dev/schema/config.json",
		"# settings for all workflows",
		"# variable comment",
		"# array item comment",
		"# legacy value comment",
		"# legacy table comment",
		"# policy setting comment",
		"# policy inline comment",
		"# canonical setting comment",
		"# canonical inline comment",
	} {
		if !bytes.Contains(migration.Content, []byte(comment)) {
			t.Errorf("migration dropped %q:\n%s", comment, migration.Content)
		}
		if count := bytes.Count(migration.Content, []byte(comment)); count != 1 {
			t.Errorf("migration emitted %q %d times, want once", comment, count)
		}
	}
	if !bytes.Contains(migration.Content, []byte("NAME#VALUE")) {
		t.Fatalf("string containing # changed:\n%s", migration.Content)
	}
	if !bytes.Contains(migration.Content, []byte("plugins = [] # unchanged root statement\r\n")) {
		t.Fatalf("unrelated statement changed:\n%s", migration.Content)
	}
	if !bytes.Contains(migration.Content, []byte("\r\n")) || strings.Contains(strings.ReplaceAll(string(migration.Content), "\r\n", ""), "\n") {
		t.Fatalf("CRLF changed:\n%q", migration.Content)
	}
	second, err := MigrateDocument("actionlint.toml", migration.Content)
	if err != nil || second.Changed || !bytes.Equal(migration.Content, second.Content) {
		t.Fatalf("TOML migration is not idempotent: %v\n%s", err, second.Content)
	}

	before, err := configtree.Decode("actionlint.toml", original)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := migrateNode(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := configtree.Decode("actionlint.toml", migration.Content)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual map[string]any
	if err := canonical.Decode(&expected); err != nil {
		t.Fatal(err)
	}
	if err := after.Decode(&actual); err != nil {
		t.Fatal(err)
	}
	encodedExpected, err := toml.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var tomlExpected map[string]any
	if err := toml.Unmarshal(encodedExpected, &tomlExpected); err != nil {
		t.Fatal(err)
	}
	encodedActual, err := toml.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedExpected, encodedActual) {
		t.Fatalf("TOML migration changed decoded settings:\nexpected: %#v\nactual: %#v", tomlExpected, actual)
	}
}

func TestTOMLMigrationCanonicalLintForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		lint string
	}{
		{name: "tables", lint: "[lint.rules.correctness]\ncache-operation = \"on\"\n"},
		{name: "inline", lint: "lint.rules = { correctness = { cache-operation = \"on\" } }\n"},
		{name: "dotted", lint: "lint.rules.correctness.cache-operation = \"on\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("#:schema https://actionlint.kjanat.dev/schema/config.json\n" +
				"config-secrets = [\"TOKEN#VALUE\"]\n" +
				"# keep canonical note\n" + tc.lint +
				"[policy]\nrequire-commit-hash = false\n")
			migration, err := MigrateDocument("actionlint.toml", source)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(migration.Content, []byte("#:schema https://actionlint.kjanat.dev/schema/config.json\n")) {
				t.Fatalf("schema header moved:\n%s", migration.Content)
			}
			if !bytes.Contains(migration.Content, []byte("# keep canonical note")) {
				t.Fatalf("canonical comment dropped:\n%s", migration.Content)
			}
			if !bytes.Contains(migration.Content, []byte("TOKEN#VALUE")) {
				t.Fatalf("quoted hash changed:\n%s", migration.Content)
			}
			root, err := configtree.Decode("actionlint.toml", migration.Content)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := resolveNode(root, nil, false)
			if err != nil {
				t.Fatalf("%v\n%s", err, migration.Content)
			}
			cfg := resolved.Config
			if cfg.RequiresCommitHash() || DiagnosticLevel(cfg, "cache-operation") != "on" || !reflect.DeepEqual(cfg.ConfigSecrets, []string{"TOKEN#VALUE"}) {
				t.Fatalf("migration semantics: %+v", cfg)
			}
		})
	}
}

func TestTOMLMigrationMovesCommentWithLintTable(t *testing.T) {
	original := []byte("#:schema https://actionlint.kjanat.dev/schema/config.json\n" +
		"config-secrets = [\"TOKEN\"]\n" +
		"# lint rule defaults\n" +
		"[lint.rules.correctness]\n" +
		"cache-operation = \"on\"\n" +
		"[tool-settings]\n" +
		"enabled = false\n")
	migration, err := MigrateDocument("actionlint.toml", original)
	if err != nil {
		t.Fatal(err)
	}
	comment := bytes.Index(migration.Content, []byte("# lint rule defaults"))
	lint := bytes.Index(migration.Content, []byte("[lint"))
	tools := bytes.Index(migration.Content, []byte("[tool-settings]"))
	if comment < 0 || lint < 0 || tools < 0 || tools > comment || comment > lint {
		t.Fatalf("lint comment detached from migrated lint subtree:\n%s", migration.Content)
	}
	if !bytes.HasPrefix(migration.Content, []byte("#:schema https://actionlint.kjanat.dev/schema/config.json\n")) {
		t.Fatalf("schema directive moved:\n%s", migration.Content)
	}
}

func TestTOMLMigrationAllLegacyRootsAndUntouchedTable(t *testing.T) {
	original := []byte(`#:schema https://actionlint.kjanat.dev/schema/config.json
description = """line one
# this is multiline string data
line three"""
self-hosted-runner = { labels = [
  # runner label comment
  "self-hosted", # second runner comment
] }
config-variables = ["OLD"]
config-secrets = ["TOKEN"]
assume-default-permissions = "permissive"
paths = { "**/*.yml" = { ignore = [
  # path comment
  "legacy.*message",
] } }
policy = { cache-operation = false, require-job-timeout = { min-minutes = 5, max-minutes = 30 }, required-actions = [] }
[lint.rules.correctness]
expression-reference = { level = "on", options = { variables = ["OLD"] } }
# first tools comment
# second tools comment
[tool-settings]
value = "kept"
`)
	migration, err := MigrateDocument("actionlint.toml", original)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{
		"#:schema https://actionlint.kjanat.dev/schema/config.json",
		"# runner label comment",
		"# second runner comment",
		"# path comment",
		"# first tools comment",
		"# second tools comment",
	} {
		if count := bytes.Count(migration.Content, []byte(comment)); count != 1 {
			t.Errorf("comment %q appeared %d times, want once:\n%s", comment, count, migration.Content)
		}
	}
	for _, preserved := range []string{
		"description = \"\"\"line one\n# this is multiline string data\nline three\"\"\"\n",
		"# first tools comment\n# second tools comment\n[tool-settings]\nvalue = \"kept\"\n",
	} {
		if !bytes.Contains(migration.Content, []byte(preserved)) {
			t.Errorf("unrelated TOML bytes changed; missing %q:\n%s", preserved, migration.Content)
		}
	}
	before, err := configtree.Decode("actionlint.toml", original)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := migrateNode(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := configtree.Decode("actionlint.toml", migration.Content)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual map[string]any
	if err := canonical.Decode(&expected); err != nil {
		t.Fatal(err)
	}
	if err := after.Decode(&actual); err != nil {
		t.Fatal(err)
	}
	encodedExpected, err := toml.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var tomlExpected map[string]any
	if err := toml.Unmarshal(encodedExpected, &tomlExpected); err != nil {
		t.Fatal(err)
	}
	encodedActual, err := toml.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedExpected, encodedActual) {
		t.Fatalf("migration changed decoded legacy settings:\nexpected: %#v\nactual: %#v", tomlExpected, actual)
	}
	resolved, err := resolveNode(after, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	timeout := resolved.Config.RequiresJobTimeout()
	minimum, hasMinimum := timeout.MinMinutes()
	maximum, hasMaximum := timeout.MaxMinutes()
	if !reflect.DeepEqual(resolved.Config.SelfHostedRunner.Labels, []string{"self-hosted"}) || resolved.Config.AssumeDefaultPermissions != DefaultPermissionsAssumptionPermissive || !resolved.Config.Paths["**/*.yml"].Ignore.Match("legacy.*message") || !hasMinimum || minimum != 5 || !hasMaximum || maximum != 30 {
		t.Fatalf("legacy settings lost: %+v", resolved.Config)
	}
}
