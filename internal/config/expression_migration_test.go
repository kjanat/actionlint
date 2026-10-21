package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestLegacyExpressionMigrationReportsIndependentSelectors(t *testing.T) {
	for _, control := range []string{
		"lint: {rules: {correctness: {expression: warn}}}",
		"lint: {rules: {disable: [expression]}}",
		"ignores: [{rule: expression, file: '**'}]",
		"overrides: [{includes: ['**'], lint: {rules: {disable: [expression]}}}]",
	} {
		source := []byte("self-hosted-runner: {labels: [gpu]}\n" + control + "\n")
		migration, err := MigrateDocument("actionlint.yaml", source)
		if err != nil {
			t.Fatal(err)
		}
		if !migration.Changed || len(migration.Notes) != 1 || !strings.Contains(migration.Notes[0], "independent rule IDs") {
			t.Fatalf("missing upgrade notice for %s: %+v", control, migration)
		}
		cfg, err := Parse(migration.Content)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(control, "expression: warn") && (DiagnosticLevel(cfg, "expression") != "warn" || DiagnosticLevel(cfg, "expression-reference") == "warn") {
			t.Fatal("migration silently expanded the expression selector")
		}
		second, err := MigrateDocument("actionlint.yaml", migration.Content)
		if err != nil || second.Changed || len(second.Notes) != 0 || !bytes.Equal(second.Content, migration.Content) {
			t.Fatalf("non-idempotent conversion: %+v, %v", second, err)
		}
	}
}

func TestCanonicalExpressionSelectionIsNotTreatedAsLegacy(t *testing.T) {
	source := []byte("lint: {rules: {correctness: {expression: off}}}\n")
	migration, err := MigrateDocument("actionlint.yaml", source)
	if err != nil || migration.Changed || len(migration.Notes) != 0 || !bytes.Equal(migration.Content, source) {
		t.Fatalf("canonical selection changed: %+v, %v", migration, err)
	}
}
