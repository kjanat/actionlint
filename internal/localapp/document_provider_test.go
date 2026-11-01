package localapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/rule"
)

func TestDocumentPluginProcessHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_DOCUMENT_HELPER") == "" {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	var request analyzer.DocumentRequest
	if err := json.Unmarshal(data, &request); err != nil {
		os.Exit(2)
	}
	if request.Kind != "workflow" || !strings.Contains(request.Source, "${{ github.ref }}") {
		os.Exit(3)
	}
	response := struct {
		OK          bool                        `json:"ok"`
		Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
	}{true, []analyzer.ScriptDiagnostic{{Filename: request.Filename, Code: "DOC1", Message: "captured document", Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 3}, Fixes: []analyzer.ScriptFix{{Description: "replace token", Replacements: []analyzer.ScriptReplacement{{Row: 1, Column: 1, EndRow: 1, EndColumn: 3, Text: "ON"}}}}}}}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func nativeDocumentManifest(t *testing.T) provider.Manifest {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(executable)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return provider.Manifest{
		SchemaVersion: 1, ID: "document-fixture", Version: "1.0.0", Role: provider.RoleDocument, Scope: provider.ScopeSource, Transport: provider.TransportProcess, Preparation: "generic",
		Rule:         rule.Descriptor{Name: "document-fixture", Description: "Whole document fixture", Category: "correctness", Subcategory: "workflow", Maturity: "stable", Provider: "document-fixture", DocumentationURL: "https://example.com/document-fixture", FixAvailability: "suggested"},
		Capabilities: provider.Capabilities{Fixes: true},
		Selectors:    provider.Selectors{Documents: []string{"workflow"}},
		Process:      &provider.ProcessSpec{Entry: filepath.Base(executable), Args: []string{"-test.run=^TestDocumentPluginProcessHelper$"}, Stdin: provider.StdinRequest, Env: map[string]string{"ACTIONLINT_DOCUMENT_HELPER": "1"}, Config: provider.ConfigRendering{Render: provider.RenderNone}, Output: provider.Output{Format: provider.FormatEnvelope}},
	}
}

func documentPluginSources(t *testing.T, manifest provider.Manifest) PluginSources {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	location := "https://plugins.example.invalid/document.json#sha256=" + hex.EncodeToString(sum[:])
	artifact := &pluginartifact.Artifact{Info: pluginartifact.Info{Kind: pluginartifact.ProviderManifest, Source: location}, Bytes: data}
	return PluginSources{Paths: []string{location}, artifacts: map[string]*pluginartifact.Artifact{location: artifact}}
}

func TestRegistryDocumentProviderRoutingAndConsent(t *testing.T) {
	manifest := nativeDocumentManifest(t)
	sources := documentPluginSources(t, manifest)
	input := func() application.Input {
		return application.Input{Kind: application.Stdin, Reader: strings.NewReader(ociProviderInput)}
	}
	result, err := CheckSources(t.Context(), application.Options{}, input(), sources)
	if err != nil {
		t.Fatal(err)
	}
	findings := slices.DeleteFunc(slices.Clone(result.Diagnostics), func(finding diagnostic.Diagnostic) bool { return finding.Rule != "document-fixture" })
	if len(findings) != 1 || findings[0].Start != (diagnostic.Position{Line: 1, Column: 1}) || findings[0].Message != "captured document" {
		t.Fatal("document provider did not run through analyzer admission", findings)
	}
	if len(findings[0].Fixes) != 1 || findings[0].Fixes[0].Edits[0].Start != findings[0].Start || len(findings[0].Fixes[0].Revisions) != 1 {
		t.Fatal("document fixes lost captured-source admission", findings[0])
	}
	descriptors, err := RulesSources(t.Context(), sources)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(descriptors, func(descriptor rule.Descriptor) bool { return descriptor.Name == "document-fixture" }) {
		t.Fatal("document provider missing from catalogue")
	}
	sources.repository = true
	if _, err := CheckSources(t.Context(), application.Options{}, input(), sources); err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
		t.Fatal("repository native execution lacked consent gate", err)
	}
	if _, err := RequiredToolsSources(t.Context(), application.Options{}, nil, sources); err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
		t.Fatal("required tools bypassed consent", err)
	}
	if _, err := CheckSources(t.Context(), application.Options{AllowConfigProcesses: true}, input(), sources); err != nil {
		t.Fatal("consented document provider failed", err)
	}
}
