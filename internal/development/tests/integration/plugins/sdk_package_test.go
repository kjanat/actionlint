package plugins_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// The package is optional for Go-only contributors. This check exercises the
// actual TypeScript authoring CLI against the host's real manifest boundary.
func TestTypeScriptAuthoringManifest(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_TEST_PLUGIN_SDK") != "1" {
		t.Skip("set ACTIONLINT_TEST_PLUGIN_SDK=1 with Bun, SDK dependencies and ACTIONLINT pointing at the current host")
	}
	sdk, err := filepath.Abs("../../../../../plugins/sdk/typescript")
	if err != nil {
		t.Fatal(err)
	}
	bun := sdkRuntime(t, "bun")
	root := t.TempDir()
	module, err := json.Marshal(filepath.ToSlash(filepath.Join(sdk, "src", "index.ts")))
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
	command := exec.CommandContext(t.Context(), bun, filepath.Join(sdk, "src", "cli.ts"), "pack", filepath.Join(root, "actionlint.plugin.ts"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("authoring CLI: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(root, "actionlint-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifests.Parse(data)
	if err != nil {
		t.Fatalf("SDK output is not a host provider: %v\n%s", err, data)
	}
	if manifest.Process == nil || manifest.Process.Entry != "pwsh" || manifest.Process.Output.Transform == nil || manifest.Process.Output.Transform.Inline != "[]" || len(manifest.Metadata) == 0 {
		t.Fatalf("SDK dropped provider or discovery facts: %+v", manifest)
	}
}

// Callback publication must remain executable when only its JSON manifest is
// installed, outside the authoring package and its node_modules tree.
func TestTypeScriptCallbackStandaloneManifest(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_PLUGIN_SDK") != "1" {
		t.Skip("set ACTIONLINT_TEST_PLUGIN_SDK=1 with Bun, SDK dependencies and ACTIONLINT pointing at the current host")
	}
	sdk, err := filepath.Abs("../../../../../plugins/sdk/typescript")
	if err != nil {
		t.Fatal(err)
	}
	bun := sdkRuntime(t, "bun")
	node := sdkRuntime(t, "node")
	root := t.TempDir()
	module, err := json.Marshal(filepath.ToSlash(filepath.Join(sdk, "src", "index.ts")))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(node)+string(os.PathListSeparator)+os.Getenv("PATH"))
	definition := fmt.Sprintf(`import {defineDocumentProvider} from %s;
import {spawnSync} from 'node:child_process';
export default defineDocumentProvider({
 id:'author/callback',version:'1.0.0',name:'Callback',description:'Standalone callback fixture',license:'MIT',repository:'https://github.com/publisher/plugin',
 rule:{name:'callback-fixture',description:'Callback fixture',category:'correctness',subcategory:'workflow',maturity:'stable',provider:'callback-fixture'},
 documentKinds:['workflow'],requiredInputs:['source','dependencies'],env_passthrough:['PATH'],
 check(request){const child=spawnSync('node',['--version'],{encoding:'utf8'});if(child.error||child.status!==0)throw new Error('child interpreter unavailable');return [{filename:request.filename,code:'SDKCB',message:JSON.stringify({source:request.source,kind:request.kind,config:request.config,dependencies:request.dependencies,path:process.env.PATH,childVersion:child.stdout.trim()}),location:{row:1,column:1}}];}
});`, module)
	for name, content := range map[string]string{"package.json": `{"name":"actionlint-callback-example","version":"1.0.0","type":"module"}`, "actionlint.plugin.ts": definition} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pack := exec.CommandContext(t.Context(), bun, filepath.Join(sdk, "src", "cli.ts"), "pack", filepath.Join(root, "actionlint.plugin.ts"))
	if output, err := pack.CombinedOutput(); err != nil {
		t.Fatalf("SDK callback pack: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(root, "actionlint-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifests.Parse(data)
	if err != nil {
		t.Fatalf("callback manifest rejected: %v\n%s", err, data)
	}
	if manifest.Process == nil || manifest.Process.Program == nil || manifest.Process.Entry != "node" || manifest.Process.Program.Filename != "actionlint.mjs" {
		t.Fatal("callback is not self-contained", manifest.Process)
	}
	runtimeDirectory := filepath.Join(t.TempDir(), "outside authoring workspace")
	if err := os.Mkdir(runtimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDirectory, "plugin.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	pool := hostprocess.NewProcessPool(t.Context(), 1)
	command, err := pool.ConfiguredCommandRunner(manifest.Process.Entry, &processconfig.Options{Executable: &node, WorkingDir: runtimeDirectory}, false)
	if err != nil {
		t.Fatal(err)
	}
	checker, err := process.NewDocumentAnalyzer(manifest, command, process.Options{Required: true})
	if err != nil {
		t.Fatal(err)
	}
	content := "on: push\njobs:\n  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/action@v1\n"
	path := filepath.Join(runtimeDirectory, "workflow.yml")
	sum := sha256.Sum256([]byte(content))
	revision, err := sourcepos.NewRevision(path, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	dependency := workflowfacts.Dependency{Reference: sourcepos.Reference{Document: path, Revision: revision.Digest(), Node: "dependency", Address: "/jobs/lint/steps/0/uses"}, Uses: "owner/action@v1", Kind: "action", Start: sourcepos.Position{Line: 6, Column: 15}, End: sourcepos.Position{Line: 6, Column: 30}}
	input := checkinputs.Input{Path: path, Directory: runtimeDirectory, Content: content, Kind: "workflow", Options: json.RawMessage(`{"mode":"standalone"}`), Revision: revision, Coordinates: sourcepos.YAML, AvailableInputs: []rulemeta.InputKind{rulemeta.Source, rulemeta.Dependencies}, Dependencies: []workflowfacts.Dependency{dependency}}
	findings, err := checker.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 1 {
		t.Fatal("standalone callback execution", findings, err)
	}
	var observed struct {
		Source       string                           `json:"source"`
		Kind         string                           `json:"kind"`
		Config       json.RawMessage                  `json:"config"`
		Dependencies []checkinputs.DocumentDependency `json:"dependencies"`
		Path         string                           `json:"path"`
		ChildVersion string                           `json:"childVersion"`
	}
	if err := json.Unmarshal([]byte(findings[0].Message), &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Source != content || observed.Kind != "workflow" || string(observed.Config) != string(input.Options) || len(observed.Dependencies) != 1 || observed.Dependencies[0].Uses != dependency.Uses || observed.Dependencies[0].Reference != dependency.Reference || observed.Dependencies[0].Start != dependency.Start {
		t.Fatal("SDK document contract changed", observed)
	}
	if observed.Path != os.Getenv("PATH") || !strings.HasPrefix(observed.ChildVersion, "v") {
		t.Fatal("declared PATH did not reach callback/child", observed.Path, observed.ChildVersion)
	}
	entries, err := os.ReadDir(runtimeDirectory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "plugin.json" {
		t.Fatal("callback depended on copied package files", entries, err)
	}
}

func sdkRuntime(t *testing.T, name string) string {
	t.Helper()
	command, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(t.Context(), command, "-p", "process.execPath").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}
