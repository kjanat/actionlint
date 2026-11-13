package config_test

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

func TestMissingPermissionsLocations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		options map[string]any
		want    []config.PermissionLocation
		invalid bool
	}{
		{"default", map[string]any{}, nil, false},
		{"workflow", map[string]any{"require-at": "workflow"}, []config.PermissionLocation{config.WorkflowPermissions}, false},
		{"every job", map[string]any{"require-at": "every-job"}, []config.PermissionLocation{config.EveryJobPermissions}, false},
		{"both", map[string]any{"require-at": []any{"workflow", "every-job"}}, config.PermissionLocations(), false},
		{"duplicate", map[string]any{"require-at": []any{"workflow", "workflow"}}, []config.PermissionLocation{config.WorkflowPermissions}, false},
		{"empty", map[string]any{"require-at": []any{}}, nil, true},
		{"null", map[string]any{"require-at": nil}, nil, true},
		{"boolean", map[string]any{"require-at": true}, nil, true},
		{"unknown", map[string]any{"require-at": "job"}, nil, true},
		{"any", map[string]any{"require-at": "any"}, nil, true},
		{"mixed", map[string]any{"require-at": []any{"workflow", 1}}, nil, true},
		{"unknown field", map[string]any{"require-at": "workflow", "scope": "job"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var options config.MissingPermissionsOptions
			err := binding.Decode(tc.options, &options)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid options accepted")
				}
				if tc.name == "empty" && !strings.Contains(err.Error(), "off") {
					t.Fatalf("empty array error does not explain disabling the rule: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := options.Locations(); !slices.Equal(got, tc.want) {
				t.Fatalf("locations=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestMissingPermissionsMigration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, want, level string }{
		{"policy: {require-permissions: true}", "workflow", "error"},
		{"policy: {require-permissions: {scope: job}}", "every-job", "error"},
		{"policy: {require-permissions: false}", "workflow", "off"},
		{"lint: {rules: {security: {explicit-permissions: {level: warn, options: {scope: job}}}}}", "every-job", "warn"},
		{"lint: {rules: {security: {missing-workflow-permissions: on}}}", "", "warn"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			t.Parallel()
			migrated, err := checking.ConfigurationRules().MigrateDocument("actionlint.yaml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			if !migrated.Changed || !strings.Contains(string(migrated.Content), "missing-permissions:") {
				t.Fatalf("migration did not replace the rule: %s", migrated.Content)
			}
			cfg, err := checking.ConfigurationRules().Parse(migrated.Content)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := checking.ConfigurationRules().ResolveRules(config.Presets{}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got := checking.ConfigurationRules().DiagnosticLevel(resolved, "missing-permissions"); string(got) != tc.level {
				t.Fatalf("level=%s want=%s", got, tc.level)
			}
			if tc.want != "" && !slices.Equal(permissionLocations(t, resolved), []config.PermissionLocation{config.PermissionLocation(tc.want)}) {
				t.Fatalf("locations=%v want=%s", permissionLocations(t, resolved), tc.want)
			}
			again, err := checking.ConfigurationRules().MigrateDocument("actionlint.yaml", migrated.Content)
			if err != nil || again.Changed {
				t.Fatalf("migration is not idempotent: changed=%t error=%v\n%s", again.Changed, err, again.Content)
			}
		})
	}
}

func permissionLocations(t *testing.T, cfg *config.Config) []config.PermissionLocation {
	t.Helper()
	locations, err := cfg.PermissionDeclarationLocations()
	if err != nil {
		t.Fatal(err)
	}
	return locations
}

func TestLegacyPermissionsRejectNonObjectSections(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"lint: false\npolicy: {require-permissions: true}",
		"lint: {rules: false}\npolicy: {require-permissions: true}",
		"lint: []\npolicy: {require-permissions: true}",
		"lint: null\npolicy: {require-permissions: true}",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			_, err := checking.ConfigurationRules().MigrateDocument("actionlint.yaml", []byte(source))
			if err == nil {
				t.Fatal("accepted legacy settings conflicting with a non-object lint section")
			}
		})
	}
}
