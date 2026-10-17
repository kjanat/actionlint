package application_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/internal/tools/backend"
	"actionlint.kjanat.dev/internal/workspace"
	"actionlint.kjanat.dev/rule"
)

func TestIndependentProcessTool(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	registration := backend.Process(rule.Descriptor{Name: "example-script", Description: "An independent provider"}, []string{"python"}, executable, &processconfig.Options{Executable: &executable, LauncherArguments: []string{"-test.run=^TestProcessToolHelper$"}, Environment: []string{"ACTIONLINT_TEST_SCRIPT_PROVIDER=1"}})
	const content = "on: push\njobs:\n  job:\n    runs-on: ubuntu-latest\n    steps:\n      - run: missing\n        shell: python\n      - run: echo skipped\n        shell: bash\n"
	source := workspace.Source{Path: "test.yml", Content: []byte(content)}
	result, err := application.Analyze(t.Context(), application.Options{Tools: []tools.Registration{registration}}, []workspace.Source{source})
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
	failed := backend.Process(rule.Descriptor{Name: "example-script"}, []string{"python"}, executable, &processconfig.Options{Executable: &executable, LauncherArguments: []string{"-test.run=^TestProcessToolHelper$"}, Environment: []string{"ACTIONLINT_TEST_SCRIPT_PROVIDER=1", "ACTIONLINT_TEST_SCRIPT_FAILURE=1"}})
	_, err = application.Analyze(t.Context(), application.Options{Tools: []tools.Registration{failed}}, []workspace.Source{source})
	if err == nil || !strings.Contains(err.Error(), "exited with status 1") {
		t.Fatalf("accepted successful JSON from failed process: %v", err)
	}
	source.Config, err = config.Parse([]byte("lint:\n  rules:\n    plugins:\n      example-script: off\n"), "example-script")
	if err != nil {
		t.Fatal(err)
	}
	result, err = application.Analyze(t.Context(), application.Options{Tools: []tools.Registration{registration}}, []workspace.Source{source})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("disabled provider: result=%+v err=%v", result, err)
	}
}
func TestProcessToolHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_TEST_SCRIPT_PROVIDER") != "1" {
		return
	}
	var request analyzer.ScriptRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}
	if request.Source != "missing" {
		os.Exit(3)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		OK          bool                        `json:"ok"`
		Diagnostics []analyzer.ScriptDiagnostic `json:"diagnostics"`
	}{true, []analyzer.ScriptDiagnostic{{Filename: request.Filename, Code: "EX1", Message: "independent result", Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 8}}}})
	if os.Getenv("ACTIONLINT_TEST_SCRIPT_FAILURE") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}
