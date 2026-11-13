package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
)

func assertNeutralSource(t *testing.T, err error, file string, line, column int, path string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var src *config.SourceError
	if !errors.As(err, &src) {
		t.Fatalf("missing SourceError: %v", err)
	}
	if src.File != file || src.Line != line || src.Column != column {
		t.Errorf("source=%s:%d:%d, want %s:%d:%d", src.File, src.Line, src.Column, file, line, column)
	}
	var bind *binding.Error
	if !errors.As(err, &bind) {
		t.Fatalf("missing binding.Error: %v", err)
	}
	if bind.Path != path {
		t.Errorf("binding path=%q, want %q", bind.Path, path)
	}
}

func TestNeutralInheritedInvalidSourceLocation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := writeNeutralFixture(t, dir, "base.yaml", "repository:\n  runners: 5\n")
	leaf := writeNeutralFixture(t, dir, "leaf.jsonc", "{\"extends\":[\"base.yaml\"], \"repository\":{\"runners\":[]}}\n")
	_, err := checking.ConfigurationRules().Load(leaf)
	assertNeutralSource(t, err, base, 2, 12, "/repository/runners")
}

func TestNeutralUnknownRuleOptionsHaveLocations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		file, source string
		line, col    int
	}{
		{"x.yaml", "lint:\n  rules:\n    security:\n      missing-permissions:\n        level: on\n        options:\n          unexpected: true\n", 7, 11},
		{"x.json", `{"lint":{"rules":{"security":{"missing-permissions":{"level":"on","options":{"unexpected":true}}}}}}`, 1, 64},
		{"x.jsonc", "{\n // options\n \"lint\": {\"rules\":{\"security\":{\"missing-permissions\":{\"level\":\"on\",\"options\":{\"unexpected\":true}}}}}\n}", 3, 65},
		{"x.toml", "[lint.rules.security.missing-permissions]\nlevel = 'on'\n[lint.rules.security.missing-permissions.options]\nunexpected = true\n", 4, 1},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := checking.ConfigurationRules().Load(path)
			if tc.file == "x.json" || tc.file == "x.jsonc" {
				tc.col = strings.Index(strings.Split(tc.source, "\n")[tc.line-1], `"unexpected"`) + 1
			}
			assertNeutralSource(t, err, path, tc.line, tc.col, "/lint/rules/security/missing-permissions/options/unexpected")
		})
	}
}

func TestNeutralLegacyScopeErrorLocation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeNeutralFixture(t, dir, "legacy.yaml", "policy:\n  require-permissions:\n    scope: invalid\n")
	_, err := checking.ConfigurationRules().Load(path)
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("invalid legacy scope accepted: %v", err)
	}
	assertNeutralSource(t, err, path, 3, 12, "/policy/require-permissions/scope")
}
