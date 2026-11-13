package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestConfigExtends(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixturefiles.Write(t, root, "base/first.yml", `lint: {rules: {correctness: {if-cond: warn}, policy: {require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}}}}
config-variables: [first]
tools: {shellcheck: {config: .shellcheckrc}}
`)
	fixturefiles.Write(t, root, "base/second.yml", `extends: [first.yml]
config-variables: [second]
`)
	path := fixturefiles.Write(t, root, "project/actionlint.yml", `extends: [../base/first.yml, ../base/second.yml]
lint: {rules: {policy: {require-job-timeout: {level: warn, options: {max-minutes: 60}}}}}
files: {includes: ['**/*.yml'], excludes: ['**/skip.yml']}
`)
	loaded, err := checking.ConfigurationRules().Read(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Config
	resolved, err := checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	minimum, _ := resolved.RequiresJobTimeout().MinMinutes()
	maximum, _ := resolved.RequiresJobTimeout().MaxMinutes()
	if minimum != 5 || maximum != 60 || checking.ConfigurationRules().DiagnosticLevel(resolved, "if-cond") != "warn" || !slices.Equal(repoconfig.VariableNames(cfg.Facts()), []string{"second"}) {
		t.Fatalf("%+v", resolved)
	}
	if len(loaded.Provenance.Sources()) != 3 {
		t.Fatal(loaded.Provenance.Sources())
	}
	inspection, err := checking.ConfigurationRules().Inspect(repoconfig.Selection{Path: path}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(inspection.Origins["/lint/rules/correctness/if-cond/level"].File, "first.yml") {
		t.Fatal(inspection.Origins)
	}
	if origin := inspection.Origins["/lint/rules/correctness/if-cond"]; !strings.HasSuffix(origin.File, "first.yml") || origin.Line != 1 {
		t.Fatalf("lost shorthand origin during merging: %+v", origin)
	}
	if cfg.IncludesFile(".github/workflows/skip.yml", root) || !cfg.IncludesFile(".github/workflows/ci.yml", root) {
		t.Fatal(cfg.Files)
	}
	encoded, err := encodeConfigYAML(cfg.Tools["shellcheck"].Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), filepath.Join(root, "base", ".shellcheckrc")) {
		t.Fatal(string(encoded))
	}
}

func TestConfigExtendsFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content, want string }{
		{"missing", "extends: [missing.yml]", "missing.yml"},
		{"cycle", "extends: [actionlint.yml]", "cyclic"},
		{"insecure remote", "extends: ['http://example.com/config.yml']", "requires a local file or an absolute HTTPS URL"},
		{"empty", "extends: ['']", "nonempty"},
		{"nonstring", "extends: [123]", "expected a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := fixturefiles.Write(t, t.TempDir(), "actionlint.yml", tc.content)
			_, err := checking.ConfigurationRules().Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
			if tc.name == "missing" && errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing inherited config would be mistaken for absent root config")
			}
		})
	}
	t.Run("HTTPS source via reader", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		remote := "https://example.com/config.yml"
		path := fixturefiles.Write(t, root, "actionlint.yml", "extends: ["+remote+"]\n")
		loaded, err := checking.ConfigurationRules().Read(path, func(source string) ([]byte, error) {
			if source == remote {
				return []byte("plugins: []\nrepository: {variables: {BASE: null}}\n"), nil
			}
			return os.ReadFile(source)
		})
		if err != nil || !slices.Equal(loaded.Provenance.Sources(), []string{remote, path}) {
			t.Fatalf("HTTPS inheritance sources=%v error=%v", loaded, err)
		}
	})
	if _, err := checking.ConfigurationRules().Parse([]byte("extends: [base.yml]")); err == nil {
		t.Fatal("in-memory config silently ignored extends")
	}
}
