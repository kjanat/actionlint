package config_test

import (
	"fmt"
	"path/filepath"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestInheritedConfigDirectorySpellings(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"${{ configdir }}", "${{ CONFIGDIR }}", "${{  configdir }}",
		"${{configdir}}", "${{\tConFigDir\t}}",
	} {
		for _, override := range []bool{false, true} {
			root := t.TempDir()
			t.Run(fmt.Sprintf("%s/override=%v", expression, override), func(t *testing.T) {
				t.Parallel()
				setting := fmt.Sprintf("tools: {shellcheck: {config: %q}}", expression+"/.shellcheckrc")
				originKey := "/tools/shellcheck/config"
				if override {
					setting = "overrides: [{includes: [ci.yml], " + setting + "}]"
					originKey = "/overrides"
				}
				base := fixturefiles.Write(t, root, "base/base.yml", setting)
				fixturefiles.Write(t, root, "middle/config.yml", "extends: [../base/base.yml]")
				leaf := fixturefiles.Write(t, root, "project/actionlint.yml", "extends: [../middle/config.yml]")
				cfg, err := checking.ConfigurationRules().Load(leaf)
				if err != nil {
					t.Fatal(err)
				}
				effective, err := checking.ConfigurationRules().ForFile(cfg, "ci.yml", root)
				if err != nil {
					t.Fatal(err)
				}
				context := repoconfig.PathContext{ConfigDir: filepath.Dir(leaf)}
				got, err := context.Resolve(effective.Tools["shellcheck"].Config.Path())
				want := filepath.Join(filepath.Dir(base), ".shellcheckrc")
				if err != nil || got != want {
					t.Fatalf("inherited rc path = %q, %v; want %q", got, err, want)
				}
				inspection, err := checking.ConfigurationRules().Inspect(repoconfig.Selection{Path: leaf}, true)
				if err != nil {
					t.Fatal(err)
				}
				if origin := inspection.Origins[originKey]; origin.File != base {
					t.Fatalf("inherited rc origin = %+v; want %s", origin, base)
				}
			})
		}
	}
}
