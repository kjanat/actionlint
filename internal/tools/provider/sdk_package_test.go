package provider_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// The package is optional for Go-only contributors. This check exercises the
// actual TypeScript authoring CLI against the host's real manifest boundary.
func TestTypeScriptAuthoringManifest(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_PLUGIN_SDK") != "1" {
		t.Skip("set ACTIONLINT_TEST_PLUGIN_SDK=1 after building packages/plugin-sdk")
	}
	sdk, err := filepath.Abs("../../../packages/plugin-sdk")
	if err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	module, err := json.Marshal(filepath.ToSlash(filepath.Join(sdk, "dist", "index.js")))
	if err != nil {
		t.Fatal(err)
	}
	definition := fmt.Sprintf(`import { defineProcessProvider } from %s;
export default defineProcessProvider({
  id: 'author/example', version: '1.0.0', name: 'Example wrapper',
  description: 'Cross-host authoring fixture', license: 'MIT',
  repository: 'https://github.com/publisher/plugin', shells: ['pwsh', 'powershell'],
  rule: {name: 'example-wrapper', description: 'Example', category: 'correctness', subcategory: 'powershell', maturity: 'stable', provider: 'example-wrapper'},
  entry: 'pwsh', args: ['-NoProfile'], stdin: 'source', config: {render: 'none'},
  output: {format: 'json', transform: {language: 'jq', inline: '[]'}}
});
`, module)
	for name, content := range map[string]string{"package.json": `{"name":"actionlint-author-example","version":"1.0.0","type":"module"}`, "actionlint.plugin.ts": definition} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.CommandContext(t.Context(), node, filepath.Join(sdk, "bin", "actionlint-plugin.js"), "pack", filepath.Join(root, "actionlint.plugin.ts"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("authoring CLI: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(root, "actionlint-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := provider.Parse(data)
	if err != nil {
		t.Fatalf("SDK output is not a host provider: %v\n%s", err, data)
	}
	if manifest.Process == nil || manifest.Process.Entry != "pwsh" || manifest.Process.Output.Transform == nil || manifest.Process.Output.Transform.Inline != "[]" || len(manifest.Metadata) == 0 {
		t.Fatalf("SDK dropped provider or discovery facts: %+v", manifest)
	}
}
