package checking_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestCheckProjectsIsolatesRepositoryPlugins(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	parent := t.TempDir()
	registry := fixtureregistry.NewPluginRegistry(t)
	for _, name := range []string{"first", "second"} {
		root := filepath.Join(parent, name)
		command := exec.CommandContext(t.Context(), "git", "init", "--quiet", root)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, output)
		}
		if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o700); err != nil {
			t.Fatal(err)
		}
		manifest := nativeDocumentManifest(t)
		manifest.ID = "example.com/" + name
		manifest.Rule.Name = "document-" + name
		manifest.Rule.Provider = manifest.Rule.Name
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		url := "https://catalogue.example/providers/" + name + ".json"
		registry.SetFile(url, data)
		digest := sha256.Sum256(data)
		fixturefiles.WriteText(t, filepath.Join(root, ".github", "actionlint.yaml"), "plugins: ['"+url+"#sha256="+hex.EncodeToString(digest[:])+"']\ntools: {shellcheck: false, ruff: false}\n")
		fixturefiles.WriteText(t, filepath.Join(root, ".github", "workflows", "ci.yml"), ociProviderInput)
	}
	for _, paths := range [][]string{{parent}, {filepath.Join(parent, "*"), parent}} {
		result, err := checking.CheckProjects(t.Context(), checking.Options{WorkingDir: parent, AllowConfigProcesses: true}, inputmodel.Request{Kind: inputmodel.Files, Paths: paths}, plugininstall.Selection{RuleSet: checking.ConfigurationRules()})
		if err != nil {
			t.Fatal(err)
		}
		if result.FileCount != 2 || len(result.Configurations) != 2 || len(result.Diagnostics) != len(result.Sources) {
			t.Fatalf("batch lost selection, config or source alignment: %+v", result)
		}
		counts := make(map[string]int)
		for _, finding := range result.Diagnostics {
			if !strings.HasPrefix(finding.Rule, "document-") {
				continue
			}
			name := strings.TrimPrefix(finding.Rule, "document-")
			if !strings.HasPrefix(finding.Path, name+string(filepath.Separator)) || finding.Code != "DOC1" || finding.Reference == nil {
				t.Fatalf("repository plugin escaped its source scope: %+v", finding)
			}
			counts[name]++
		}
		if counts["first"] != 1 || counts["second"] != 1 {
			t.Fatalf("plugins missing or ran in other repositories: %v", counts)
		}
	}
}
