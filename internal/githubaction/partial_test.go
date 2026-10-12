package githubaction

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	if strings.Contains(string(script), "HANG_ANALYZER") {
		if err := os.WriteFile(os.Getenv("ACTIONLINT_PARTIAL_RESULT_READY"), nil, 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Minute)
		os.Exit(2)
	}
	fmt.Print(`{"comments":[{"file":"-","line":2,"endLine":2,"column":6,"endColumn":12,"level":"warning","code":2086,"message":"Quote this variable."}]}`)
	os.Exit(1)
}

func TestActionPreservesNativeTimeoutFindings(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_PARTIAL_RESULT_HELPER", "1")
	t.Setenv("SHELLCHECK_OPTS", "")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("ACTIONLINT_PARTIAL_RESULT_READY", ready)
	workspace := workspaceWith(t, map[string]string{
		"timeout.yaml": strings.ReplaceAll(brokenWorkflow, "echo hi", "echo HANG_ANALYZER"),
	})
	resultPath := filepath.Join(t.TempDir(), "result.json")
	env := map[string]string{
		"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": filepath.Join(t.TempDir(), "outputs"),
		"ACTIONLINT_ACTION_RESULT": resultPath,
	}
	var output strings.Builder
	a := &action{
		args:   args("./timeout.yaml", "json", "", "", "true", "false", ".", "", "true"),
		stdout: &output, env: func(name string) string { return env[name] },
		newID: fixedID("DELIM"), timeout: 2 * time.Second,
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
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("analyzer did not start before timeout: %v", err)
	}
	var result persistedResult
	if err := json.Unmarshal([]byte(read(t, resultPath)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Completed || result.ExitCode != actionlint.ExitStatusFailure || result.Status != "failure" || result.Error != "actionlint timed out after 2 seconds\n" {
		t.Fatalf("lost timeout status: %+v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "./timeout.yaml" || result.Diagnostics[0].Rule != "runner-label" {
		t.Fatalf("lost partial finding: %+v", result.Diagnostics)
	}
	if result.FileCount == nil || *result.FileCount != 1 {
		t.Fatalf("lost selected file count: %+v", result.FileCount)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("timeout discarded parsed outline: %+v", result.Documents)
	}
	workflow, ok := result.Documents[0].(actionlint.WorkflowOutline)
	if !ok || workflow.Path != "./timeout.yaml" || workflow.ParseStatus != "complete" || len(workflow.Jobs) != 1 {
		t.Fatalf("timeout corrupted workflow document: %+v", result.Documents)
	}
	var emitted persistedResult
	outputs := parseOutputs(read(t, env["GITHUB_OUTPUT"]))
	if err := json.Unmarshal([]byte(outputs["output"]), &emitted); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if !reflect.DeepEqual(result, emitted) {
		t.Fatalf("JSON output differs from persisted timeout: %+v != %+v", emitted, result)
	}
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
			var emitted persistedResult
			outputs := parseOutputs(read(t, env["GITHUB_OUTPUT"]))
			if err := json.Unmarshal([]byte(outputs["output"]), &emitted); err != nil {
				t.Fatalf("invalid JSON output: %v", err)
			}
			if !reflect.DeepEqual(result, emitted) {
				t.Fatalf("JSON output differs from persisted failure: %+v != %+v", emitted, result)
			}
		})
	}
}
