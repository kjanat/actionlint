package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestConfigExtends(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFixture(t, root, "base/first.yml", `lint: {rules: {correctness: {if-cond: warn}, policy: {require-job-timeout: {level: error, options: {min-minutes: 5, max-minutes: 30}}}}}
config-variables: [first]
tools: {shellcheck: {config: .shellcheckrc}}
`)
	testutil.WriteFixture(t, root, "base/second.yml", `extends: [first.yml]
config-variables: [second]
`)
	path := testutil.WriteFixture(t, root, "project/actionlint.yml", `extends: [../base/first.yml, ../base/second.yml]
lint: {rules: {policy: {require-job-timeout: {level: warn, options: {max-minutes: 60}}}}}
files: {includes: ['**/*.yml'], excludes: ['**/skip.yml']}
`)
	cfg, err := repoconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved := repoconfig.ResolveRules(repoconfig.Presets{}, cfg)
	minimum, _ := resolved.RequiresJobTimeout().MinMinutes()
	maximum, _ := resolved.RequiresJobTimeout().MaxMinutes()
	if minimum != 5 || maximum != 60 || repoconfig.DiagnosticLevel(resolved, "if-cond") != "warn" || !slices.Equal(cfg.ConfigVariables, []string{"second"}) {
		t.Fatalf("%+v", resolved)
	}
	if len(cfg.Sources()) != 3 {
		t.Fatal(cfg.Sources())
	}
	inspection, err := repoconfig.Inspect(repoconfig.Selection{Path: path}, true)
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
	encoded, err := codec.Marshal(cfg.Tools["shellcheck"].Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), filepath.Join(root, "base", ".shellcheckrc")) {
		t.Fatal(string(encoded))
	}
}

func TestConfigExtendsFailures(t *testing.T) {
	for _, tc := range []struct{ name, content, want string }{
		{"missing", "extends: [missing.yml]", "missing.yml"},
		{"cycle", "extends: [actionlint.yml]", "cyclic"},
		{"remote", "extends: ['https://example.com/config.yml']", "local file"},
		{"empty", "extends: ['']", "nonempty"},
		{"nonstring", "extends: [123]", "strings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testutil.WriteFixture(t, t.TempDir(), "actionlint.yml", tc.content)
			_, err := repoconfig.Load(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal(err)
			}
			if tc.name == "missing" && errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing inherited config would be mistaken for absent root config")
			}
		})
	}
	if _, err := repoconfig.Parse([]byte("extends: [base.yml]")); err == nil {
		t.Fatal("in-memory config silently ignored extends")
	}
}
