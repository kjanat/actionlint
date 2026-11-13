package checking_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestDocumentPluginProcessHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_DOCUMENT_HELPER") == "" {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	var request checkinputs.DocumentRequest
	if err := json.Unmarshal(data, &request); err != nil {
		os.Exit(2)
	}
	if request.Kind != "workflow" || !strings.Contains(request.Source, "${{ github.ref }}") {
		os.Exit(3)
	}
	response := struct {
		OK          bool                           `json:"ok"`
		Diagnostics []checkinputs.ScriptDiagnostic `json:"diagnostics"`
	}{true, []checkinputs.ScriptDiagnostic{{Filename: request.Filename, Code: "DOC1", Message: "captured document", Location: checkinputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: checkinputs.ScriptPosition{Row: 1, Column: 3}, Fixes: []checkinputs.ScriptFix{{Description: "replace token", Replacements: []checkinputs.ScriptReplacement{{Row: 1, Column: 1, EndRow: 1, EndColumn: 3, Text: "ON"}}}}}}}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func nativeDocumentManifest(t *testing.T) manifests.Manifest {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return manifests.Manifest{
		SchemaVersion: 1, ID: "document-fixture", Version: "1.0.0", Role: manifests.RoleDocument, Scope: manifests.ScopeSource, Transport: manifests.TransportProcess, Preparation: "generic",
		Rule:         rule.Descriptor{Name: "document-fixture", Description: "Whole document fixture", Category: "correctness", Subcategory: "workflow", Maturity: "stable", Provider: "document-fixture", DocumentationURL: "https://example.com/document-fixture"},
		Capabilities: manifests.Capabilities{Fixes: true},
		Selectors:    manifests.Selectors{Documents: []string{"workflow"}},
		Process:      &manifests.ProcessSpec{Entry: executable, Args: []string{"-test.run=^TestDocumentPluginProcessHelper$"}, Stdin: manifests.StdinRequest, Env: map[string]string{"ACTIONLINT_DOCUMENT_HELPER": "1"}, Config: manifests.ConfigRendering{Render: manifests.RenderNone}, Output: manifests.Output{Format: manifests.FormatEnvelope}},
	}
}

func documentPluginSources(t *testing.T, manifest manifests.Manifest) plugininstall.Sources {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	location := "https://plugins.example.invalid/document.json#sha256=" + hex.EncodeToString(sum[:])
	artifact := &pluginartifact.Artifact{Info: pluginartifact.Info{Kind: pluginartifact.ProviderManifest, Source: location}, Bytes: data}
	return plugininstall.Sources{Paths: []string{location}, Artifacts: map[string]*pluginartifact.Artifact{location: artifact}}
}

func TestRegistryDocumentProviderRoutingAndConsent(t *testing.T) {
	t.Parallel()
	manifest := nativeDocumentManifest(t)
	sources := documentPluginSources(t, manifest)
	input := func() inputmodel.Request {
		return inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(ociProviderInput)}
	}
	result, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, input(), sources)
	if err != nil {
		t.Fatal(err)
	}
	findings := slices.DeleteFunc(slices.Clone(result.Diagnostics), func(finding results.Diagnostic) bool { return finding.Rule != "document-fixture" })
	if len(findings) != 1 || findings[0].Start != (sourcepos.Position{Line: 1, Column: 1}) || findings[0].Message != "captured document" {
		t.Fatal("document provider did not run through analyzer admission", findings)
	}
	if len(findings[0].Fixes) != 1 || findings[0].Fixes[0].Edits[0].Start != findings[0].Start || len(findings[0].Fixes[0].Revisions) != 1 {
		t.Fatal("document fixes lost captured-source admission", findings[0])
	}
	descriptors, err := checking.RulesWithPlugins(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(descriptors, func(descriptor rule.Descriptor) bool { return descriptor.Name == "document-fixture" }) {
		t.Fatal("document provider missing from catalogue")
	}
	sources.Configured = true
	if _, err := checking.CheckWithPlugins(t.Context(), checking.Options{}, input(), sources); err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
		t.Fatal("repository native execution lacked consent gate", err)
	}
	if _, err := checking.RequiredToolsWithPlugins(t.Context(), checking.Options{}, nil, sources); err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
		t.Fatal("required tools bypassed consent", err)
	}
	if _, err := checking.CheckWithPlugins(t.Context(), checking.Options{AllowConfigProcesses: true}, input(), sources); err != nil {
		t.Fatal("consented document provider failed", err)
	}
}
