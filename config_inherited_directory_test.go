package actionlint

import (
	"fmt"
	"path/filepath"
	"testing"
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
				effective, err := configForFile(cfg, "ci.yml", root)
				if err != nil {
					t.Fatal(err)
				}
				context := configPathContext{configDir: filepath.Dir(leaf)}
				got, err := context.resolve(effective.Tools.Shellcheck.Config.path())
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

func TestInheritedConfigDirectoryLateBoundPaths(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	for _, expression := range []string{
		"${{ GITDIR }}", "${{  github.workspace }}", "${{github.action_path}}",
	} {
		t.Run(expression, func(t *testing.T) {
			input := expression + "/${{ CONFIGDIR }}/${{\tconfigdir\t}}/.shellcheckrc"
			want := expression + "/" + base + "/" + base + "/.shellcheckrc"
			if got := expandInheritedConfigDirectory(input, base); got != want {
				t.Fatalf("expanded path = %q; want %q", got, want)
			}
		})
	}
}
