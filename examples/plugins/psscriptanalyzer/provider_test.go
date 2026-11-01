package psscriptanalyzer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func load(t *testing.T) (provider.Manifest, provider.Programs) {
	t.Helper()
	manifest, programs, err := provider.ReadLocal(fsread.New(t.Context(), nil, nil), "provider.json")
	if err != nil {
		t.Fatal(err)
	}
	return manifest, programs
}

func TestRecordedPSScriptAnalyzerOutput(t *testing.T) {
	manifest, _ := load(t)
	if manifest.Process == nil || manifest.Process.Output.Transform == nil {
		t.Fatal("missing process output transform")
	}
	program, err := os.ReadFile("output.jq")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Process.Output.Transform.Inline != string(program) {
		t.Fatal("inline published transform differs from output.jq")
	}
	decoder, err := process.NewJQ(string(program), process.JQJSON)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile("testdata/alias.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := decoder.Decode(t.Context(), output, map[string]string{"filename": "actionlint.ps1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatal(result)
	}
	finding := result.Diagnostics[0]
	if finding.Code != "PSAvoidUsingCmdletAliases" || finding.Severity != "warning" || finding.Location.Column != 20 || finding.EndLocation.Column != 23 {
		t.Fatal(finding)
	}
	if len(finding.Fixes) != 1 || len(finding.Fixes[0].Replacements) != 1 || finding.Fixes[0].Replacements[0].Text != "Get-ChildItem" {
		t.Fatal(finding.Fixes)
	}
	clean, err := decoder.Decode(t.Context(), []byte("[]"), map[string]string{"filename": "actionlint.ps1"})
	if err != nil || len(clean.Diagnostics) != 0 {
		t.Fatalf("clean output: %+v, %v", clean, err)
	}
}

// Opt in when pwsh 7 and PSScriptAnalyzer >=1.25.0 are installed. This never
// installs a dependency or downloads an artifact during a test.
func TestPSScriptAnalyzerWorkflow(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_PSSCRIPTANALYZER") != "1" {
		t.Skip("set ACTIONLINT_TEST_PSSCRIPTANALYZER=1 with pwsh and PSScriptAnalyzer installed")
	}
	manifest, programs := load(t)
	registration, err := backend.RegisterProvider(backend.Provider{Manifest: manifest, Programs: programs, Selection: backend.Selection{Command: "pwsh"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repoconfig.Parse([]byte("plugins: []\ntools: {psscriptanalyzer: {config: {IncludeRules: [PSAvoidUsingCmdletAliases]}}}\n"), "psscriptanalyzer")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := os.ReadFile("workflow.yml")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: []tools.Registration{registration}}, []workspace.Source{{Path: filepath.Join(root, "workflow.yml"), Content: workflow, Config: cfg}})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, finding := range result.Diagnostics {
		if finding.Rule != "psscriptanalyzer" {
			continue
		}
		found++
		if !strings.Contains(finding.Message, "PSAvoidUsingCmdletAliases") {
			t.Fatal(finding)
		}
		switch finding.Start.Line {
		case 10:
			if finding.Start.Column != 29 {
				t.Fatalf("UTF-16 column was not mapped through the YAML scalar: %+v", finding)
			}
		case 14:
			if finding.Start.Column != 11 {
				t.Fatal(finding)
			}
		default:
			t.Fatalf("unexpected workflow location: %+v", finding)
		}
	}
	if found != 2 {
		t.Fatalf("expected both pwsh and powershell snippets, got %d: %+v", found, result.Diagnostics)
	}
	pool := analyzerhost.NewProcessPool(t.Context(), 1)
	command, err := pool.NewCommandRunner("pwsh", false)
	if err != nil {
		t.Fatal(err)
	}
	native, err := process.NewBackend(manifest, command, process.Options{Programs: programs, Required: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"Get-ChildItem .", "gci ."} {
		config := []byte(`{"ExcludeRules":["PSAvoidUsingCmdletAliases"]}`)
		findings, err := native.Check(t.Context(), analyzer.ScriptRequest{Source: source, Filename: "actionlint.ps1", Config: config, Directory: root})
		if err != nil || len(findings) != 0 {
			t.Fatalf("clean or excluded snippet %q: %+v, %v", source, findings, err)
		}
	}
	findings, err := native.Check(t.Context(), analyzer.ScriptRequest{Source: "Write-Output (", Filename: "actionlint.ps1", Directory: root})
	if err != nil || len(findings) == 0 || findings[0].Severity != "error" {
		t.Fatalf("PowerShell parse failure: %+v, %v", findings, err)
	}
}
