package plugins_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	hostprocess "actionlint.kjanat.dev/v2/internal/integrations/process"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

const psscriptanalyzerExample = "../../../../../plugins/examples/psscriptanalyzer"

func loadPSScriptAnalyzer(t *testing.T) (manifests.Manifest, manifests.Programs) {
	t.Helper()
	manifest, programs, err := manifests.ReadLocal(files.New(t.Context(), nil, nil), filepath.Join(psscriptanalyzerExample, "provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	return manifest, programs
}

func TestRecordedPSScriptAnalyzerOutput(t *testing.T) {
	t.Parallel()
	manifest, _ := loadPSScriptAnalyzer(t)
	if manifest.Process == nil || manifest.Process.Output.Transform == nil {
		t.Fatal("missing process output transform")
	}
	program, err := os.ReadFile(filepath.Join(psscriptanalyzerExample, "output.jq"))
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
	output, err := os.ReadFile(filepath.Join(psscriptanalyzerExample, "testdata", "alias.json"))
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
	t.Parallel()
	if os.Getenv("ACTIONLINT_TEST_PSSCRIPTANALYZER") != "1" {
		t.Skip("set ACTIONLINT_TEST_PSSCRIPTANALYZER=1 with pwsh and PSScriptAnalyzer installed")
	}
	manifest, programs := loadPSScriptAnalyzer(t)
	registration, err := scripts.RegisterProvider(scripts.Provider{Manifest: manifest, Programs: programs, Selection: scripts.Selection{Command: "pwsh"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := checking.ConfigurationRules().Parse([]byte("plugins: []\ntools: {psscriptanalyzer: {config: {IncludeRules: [PSAvoidUsingCmdletAliases]}}}\n"), "psscriptanalyzer")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := os.ReadFile(filepath.Join(psscriptanalyzerExample, "workflow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root, Tools: []tools.Registration{registration}}, []workspace.Source{{Path: filepath.Join(root, "workflow.yml"), Content: workflow, Config: cfg}})
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
	pool := hostprocess.NewProcessPool(t.Context(), 1)
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
		findings, err := native.Check(t.Context(), checkinputs.ScriptRequest{Source: source, Filename: "actionlint.ps1", Config: config, Directory: root})
		if err != nil || len(findings) != 0 {
			t.Fatalf("clean or excluded snippet %q: %+v, %v", source, findings, err)
		}
	}
	findings, err := native.Check(t.Context(), checkinputs.ScriptRequest{Source: "Write-Output (", Filename: "actionlint.ps1", Directory: root})
	if err != nil || len(findings) == 0 || findings[0].Severity != "error" {
		t.Fatalf("PowerShell parse failure: %+v, %v", findings, err)
	}
}
