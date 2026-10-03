package githubaction

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestPartialResultAnalyzer(t *testing.T) {
	if os.Getenv("ACTIONLINT_PARTIAL_RESULT_HELPER") != "1" {
		return
	}
	script, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	if strings.Contains(string(script), "CRASH_ANALYZER") {
		fmt.Fprintln(os.Stderr, "simulated analyzer failure")
		os.Exit(2)
	}
	fmt.Print(`{"comments":[{"file":"-","line":2,"endLine":2,"column":6,"endColumn":12,"level":"warning","code":2086,"message":"Quote this variable."}]}`)
	os.Exit(1)
}

func TestActionPreservesNativePartialFindings(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_PARTIAL_RESULT_HELPER", "1")
	t.Setenv("SHELLCHECK_OPTS", "")
	workspace := workspaceWith(t, map[string]string{
		"finding.yaml": cleanWorkflow,
		"failure.yaml": strings.ReplaceAll(cleanWorkflow, "echo hi", "echo CRASH_ANALYZER"),
		"both.yaml":    strings.ReplaceAll(brokenWorkflow, "echo hi", "echo CRASH_ANALYZER"),
	})
	for _, tc := range []struct {
		name, files, path, rule string
	}{
		{"finding first", "./finding.yaml\nfailure.yaml", "./finding.yaml", "shellcheck"},
		{"failure first", "failure.yaml\n./finding.yaml", "./finding.yaml", "shellcheck"},
		{"same workflow", "./both.yaml", "./both.yaml", "runner-label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resultPath := filepath.Join(t.TempDir(), "result.json")
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": filepath.Join(t.TempDir(), "outputs"),
				"ACTIONLINT_ACTION_RESULT": resultPath,
			}
			var output strings.Builder
			a := &action{
				args:   args(tc.files, "json", "", "", "true", "false", ".", "", "true"),
				stdout: &output, env: func(name string) string { return env[name] },
				newID: fixedID("DELIM"), timeout: lintTimeout,
				lint: func(req *lintRequest) *lintResult {
					req.shellcheckOptions = &actionlint.ExternalCommandOptions{
						Executable: &executable, Arguments: []string{"-test.run=^TestPartialResultAnalyzer$", "--"},
					}
					return runLinter(req)
				},
			}
			if code := a.run(); code != actionlint.ExitStatusFailure {
				t.Fatalf("exit = %d, want failure: %s", code, output.String())
			}
			var result persistedResult
			if err := json.Unmarshal([]byte(read(t, resultPath)), &result); err != nil {
				t.Fatal(err)
			}
			if result.Completed || result.ExitCode != actionlint.ExitStatusFailure || result.Status != "failure" || !strings.Contains(result.Error, "simulated analyzer failure") {
				t.Fatalf("lost failure status: %+v", result)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != tc.path || result.Diagnostics[0].Rule != tc.rule {
				t.Fatalf("lost partial finding: %+v", result.Diagnostics)
			}
		})
	}
}
