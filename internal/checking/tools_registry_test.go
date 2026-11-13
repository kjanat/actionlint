package checking_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestIndependentProcessTool(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	register := func(environment ...string) tools.Registration {
		t.Helper()
		registration, err := scripts.RegisterProvider(scripts.Provider{
			Manifest: manifests.Manifest{
				SchemaVersion: manifests.SchemaVersion, ID: "example.com/example-script", Version: "1", Role: manifests.RoleScript,
				Rule: rule.Descriptor{Name: "example-script", Description: "An independent provider", Category: "correctness", Maturity: "stable"}, Scope: manifests.ScopeSource,
				Transport: manifests.TransportProcess, Selectors: manifests.Selectors{Shells: []string{"python"}},
				Process: &manifests.ProcessSpec{
					Entry: "example-script", Stdin: manifests.StdinRequest,
					// The child runs scrubbed; the test binary needs a temporary directory.
					EnvPassthrough: []string{"TMPDIR", "TEMP", "TMP"},
					Config:         manifests.ConfigRendering{Render: manifests.RenderJSONStdin},
					Output:         manifests.Output{Format: manifests.FormatEnvelope},
				},
			},
			Selection: scripts.Selection{Process: &processconfig.Options{Executable: &executable, LauncherArguments: []string{"-test.run=^TestProcessToolHelper$"}, Environment: environment}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return registration
	}
	registration := register("ACTIONLINT_TEST_SCRIPT_PROVIDER=1")
	const content = "on: push\njobs:\n  job:\n    runs-on: ubuntu-latest\n    steps:\n      - run: missing\n        shell: python\n      - run: echo skipped\n        shell: bash\npermissions: {}\n"
	source := workspace.Source{Path: "test.yml", Content: []byte(content)}
	result, err := checking.Analyze(t.Context(), checking.Options{Tools: []tools.Registration{registration}}, []workspace.Source{source})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("findings=%+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Rule != "example-script" || finding.Code != "EX1" || finding.Start.Line != 6 || finding.Start.Column != 14 || finding.End.Column != 21 {
		t.Fatalf("mapping=%+v", finding)
	}
	failed := register("ACTIONLINT_TEST_SCRIPT_PROVIDER=1", "ACTIONLINT_TEST_SCRIPT_FAILURE=1")
	_, err = checking.Analyze(t.Context(), checking.Options{Tools: []tools.Registration{failed}}, []workspace.Source{source})
	if err == nil || !strings.Contains(err.Error(), "exited with status 1") {
		t.Fatalf("accepted successful JSON from failed process: %v", err)
	}
	source.Config, err = checking.ConfigurationRules().Parse([]byte("lint:\n  rules:\n    plugins:\n      example-script: off\n"), "example-script")
	if err != nil {
		t.Fatal(err)
	}
	result, err = checking.Analyze(t.Context(), checking.Options{Tools: []tools.Registration{registration}}, []workspace.Source{source})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("disabled provider: result=%+v err=%v", result, err)
	}
}

func TestProcessToolHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_TEST_SCRIPT_PROVIDER") != "1" {
		return
	}
	var request checkinputs.ScriptRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}
	if request.Source != "missing" {
		os.Exit(3)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		OK          bool                           `json:"ok"`
		Diagnostics []checkinputs.ScriptDiagnostic `json:"diagnostics"`
	}{true, []checkinputs.ScriptDiagnostic{{Filename: request.Filename, Code: "EX1", Message: "independent result", Location: checkinputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: checkinputs.ScriptPosition{Row: 1, Column: 8}}}})
	if os.Getenv("ACTIONLINT_TEST_SCRIPT_FAILURE") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}
