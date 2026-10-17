package engine

import (
	"fmt"
	"path/filepath"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func TestInheritedConfigDirectorySpellings(t *testing.T) {
	for _, expression := range []string{
		"${{ configdir }}", "${{ CONFIGDIR }}", "${{  configdir }}",
		"${{configdir}}", "${{\tConFigDir\t}}",
	} {
		for _, override := range []bool{false, true} {
			root := t.TempDir()
			t.Run(fmt.Sprintf("%s/override=%v", expression, override), func(t *testing.T) {
				setting := fmt.Sprintf("tools: {shellcheck: {config: %q}}", expression+"/.shellcheckrc")
				originKey := "/tools/shellcheck/config"
				if override {
					setting = "overrides: [{includes: [ci.yml], " + setting + "}]"
					originKey = "/overrides"
				}
				base := writeShellcheckFixture(t, root, "base/base.yml", setting)
				writeShellcheckFixture(t, root, "middle/config.yml", "extends: [../base/base.yml]")
				leaf := writeShellcheckFixture(t, root, "project/actionlint.yml", "extends: [../middle/config.yml]")
				cfg, err := ReadConfigFile(leaf)
				if err != nil {
					t.Fatal(err)
				}
				effective, err := repoconfig.ForFile(cfg, "ci.yml", root)
				if err != nil {
					t.Fatal(err)
				}
				context := repoconfig.PathContext{ConfigDir: filepath.Dir(leaf)}
				got, err := context.Resolve(effective.Tools.Shellcheck.Config.Path())
				want := filepath.Join(filepath.Dir(base), ".shellcheckrc")
				if err != nil || got != want {
					t.Fatalf("inherited rc path = %q, %v; want %q", got, err, want)
				}
				inspection, err := InspectConfig(ConfigSelection{Path: leaf}, true)
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
