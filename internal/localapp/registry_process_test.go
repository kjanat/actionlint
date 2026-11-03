package localapp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/pluginref"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/rule"
)

// Registry installation must preserve the process boundary: install without
// executing, refuse repository execution without consent, then work offline.
func TestRegistryProcessProviderInstallAndConsent(t *testing.T) {
	testutil.IsolateUserCache(t)
	network := testutil.NewPluginRegistry(t)
	network.Add(t, "v1.0.0", []byte{0, 97, 115, 109, 13, 0, 1, 0})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const metadata = `{"metadata_version":1,"id":"no-sudo","name":"Registry process","description":"Process installation fixture","license":"MIT","repository":"https://github.com/publisher/plugin","plugin_version":"1.0.0","transport":"process"}`
	manifest := provider.Manifest{
		SchemaVersion: 1, Metadata: json.RawMessage(metadata), ID: "no-sudo", Version: "1.0.0", Role: provider.RoleScript, Scope: provider.ScopeSource, Transport: provider.TransportProcess,
		Rule:        rule.Descriptor{Name: "registry-process", Description: "Registry process fixture", Category: "correctness", Subcategory: "shell", Maturity: "stable", Provider: "registry-process", Recommended: true, DocumentationURL: "https://github.com/publisher/plugin"},
		Preparation: "kjanat:actionlint/expression-mask@0.1.0", Selectors: provider.Selectors{Shells: []string{"bash"}, Filename: "actionlint.sh"},
		Capabilities: provider.Capabilities{MessageTemplate: "{code}: {message}"},
		Process: &provider.ProcessSpec{
			Entry: executable, Args: []string{"-test.run=^TestLocalManifestToolHelper$", "--", "{var:filename}"}, Stdin: provider.StdinSource,
			Env: map[string]string{"ACTIONLINT_LOCAL_MANIFEST_HELPER": "1"}, EnvPassthrough: []string{"TEMP", "TMP", "TMPDIR"}, Config: provider.ConfigRendering{Render: provider.RenderNone},
			Output: provider.Output{Format: provider.FormatJSON, Transform: &provider.Transform{Language: provider.LanguageJQ, Inline: `[.items[] | {filename: $vars.filename, code: "NATIVE1", message: .text, location: {row: .line, column: .col}}]`}},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	const assetURL = "https://github.com/publisher/plugin/releases/download/v1.0.0/provider.json"
	submission := fmt.Sprintf(`{"schema_version":2,"provider":"github","repository":"publisher/plugin","tag":"v1.0.0","kind":"manifest","artifact":{"type":"release-asset","name":"provider.json","sha256":%q}}`, digest)
	declaration := fmt.Sprintf(`{"kind":"manifest","sha256":%q,"plugin_id":"no-sudo","plugin_version":"1.0.0","repository":"https://github.com/publisher/plugin","contract":"process/1","metadata":%s}`, digest, metadata)
	record := fmt.Sprintf(`{"record_version":2,"artifact":%s,"submission":%s,"receipt":{"schema_version":1,"submission":%s,"identity":{"issuer":"https://token.actions.githubusercontent.com","repository_id":"10","owner_id":"5","repository":"publisher/plugin","ref":"refs/tags/v1.0.0","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},"repository_id":10,"owner_id":5,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","url":%q}`, declaration, submission, submission, assetURL)
	network.SetFile(assetURL, data)
	network.SetFile(network.RecordURL("v1.0.0"), []byte(record))
	root := pluginRepository(t)
	configPath := filepath.Join(root, ".github", "actionlint.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	writeManifestTest(t, configPath, "plugins: []\n")
	project, err := OpenPluginProject(t.Context(), root, "", configPath)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pluginref.New(testutil.FixturePluginName, "v1.0.0", testutil.FixtureRegistryURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Resolve(t.Context(), []pluginref.Reference{ref}, nil); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(t.Context(), []pluginref.Reference{ref}); err != nil {
		t.Fatal(err)
	}
	sources, err := (PluginSelection{ConfigFile: configPath}).Resolve(t.Context(), root, application.Input{Kind: application.Repository})
	if err != nil {
		t.Fatal(err)
	}
	input := func() application.Input {
		return application.Input{Kind: application.Stdin, Reader: strings.NewReader(ociProviderInput)}
	}
	if _, err := CheckSources(t.Context(), application.Options{}, input(), sources); err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
		t.Fatalf("repository process did not require consent: %v", err)
	}
	network.SetOffline(true)
	result, err := CheckSources(t.Context(), application.Options{AllowConfigProcesses: true}, input(), sources)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range result.Diagnostics {
		if finding.Rule == "registry-process" && finding.Start.Line == 6 && strings.Contains(finding.Message, "NATIVE1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("installed provider did not run from cache: %+v", result.Diagnostics)
	}
}
