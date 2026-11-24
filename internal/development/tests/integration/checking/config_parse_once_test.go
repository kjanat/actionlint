package checking_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/assembly"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/fixturefiles"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
)

func TestCheckProjectsParsesConfigurationOnce(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := fixturefiles.Write(t, root, "base.yaml", "lint: {rules: {disable: [empty-run]}}\n")
	config := fixturefiles.Write(t, root, "actionlint.yaml", "extends: [./base.yaml]\nplugins: []\noutput: {formats: [github]}\n")
	workflow := fixturefiles.Write(t, root, "workflow.yml", "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ''\n")

	files := document.NewFiles()
	parses := 0
	loaded, err := checking.ConfigurationRules().ReadBeforePluginsContext(t.Context(), config, func(path string) ([]byte, error) {
		parses++
		return os.ReadFile(path)
	}, files)
	if err != nil {
		t.Fatal(err)
	}
	if parses != 2 || len(loaded.Config.Output.Formats) != 1 {
		t.Fatalf("output load parsed %d files, formats %v", parses, loaded.Config.Output.Formats)
	}
	for _, path := range []string{config, base} {
		if err := os.WriteFile(path, []byte("lint: [unterminated\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	options := checking.Options{WorkingDir: root, ConfigFile: config, SkipProjectConfig: true, ConfigSources: files}
	selection := pluginselection.PluginSelection{RuleSet: checking.ConfigurationRules(), ConfigFile: config, DisableTools: true}
	result, err := assembly.CheckProjects(t.Context(), options, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{workflow}}, selection)
	if err != nil {
		t.Fatalf("check parsed configuration again: %v", err)
	}
	if result.FileCount != 1 {
		t.Fatalf("checked %d files", result.FileCount)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule == "empty-run" {
			t.Fatalf("inherited setting was lost: %+v", diagnostic)
		}
	}
}
