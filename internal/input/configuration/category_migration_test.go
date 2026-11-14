package config_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestMovedRuleMigration(t *testing.T) {
	t.Parallel()
	rules := checking.ConfigurationRules()
	for _, level := range []string{"off", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			source := "lint: {rules: {suspicious: {empty-job: " + level + "}}}"
			migrated, err := rules.MigrateDocument("actionlint.yaml", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := rules.Parse(migrated.Content)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := rules.ResolveRules(config.Presets{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := rules.DiagnosticLevel(resolved, "empty-job"); string(got) != level {
				t.Fatalf("migrated level = %s, want %s\n%s", got, level, migrated.Content)
			}
			again, err := rules.MigrateDocument("actionlint.yaml", migrated.Content)
			if err != nil || again.Changed || !migrated.Changed {
				t.Fatalf("migration changed=%t, second migration changed=%t, error=%v", migrated.Changed, again.Changed, err)
			}
		})
	}
}

func TestMovedRuleMigrationInOverride(t *testing.T) {
	t.Parallel()
	source := "overrides: [{includes: ['ci.yml'], lint: {rules: {suspicious: {empty-job: off}}}}]"
	migrated, err := checking.ConfigurationRules().MigrateDocument("actionlint.yaml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := checking.ConfigurationRules().Parse(migrated.Content)
	if err != nil {
		t.Fatal(err)
	}
	setting, exists := cfg.Overrides[0].Lint.Rules.Correctness.Rule("empty-job")
	if !exists || setting.Level() != "off" {
		t.Fatalf("override lost empty-job disablement: %s", migrated.Content)
	}
}

func TestMovedRuleMigrationRejectsConflictingSettings(t *testing.T) {
	t.Parallel()
	source := "lint: {rules: {suspicious: {empty-job: off}, correctness: {empty-job: error}}}"
	_, err := checking.ConfigurationRules().MigrateDocument("actionlint.yaml", []byte(source))
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("conflicting settings were not reported: %v", err)
	}
}
