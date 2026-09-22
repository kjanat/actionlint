package githubaction

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestActionSARIFOutputRetainsStructuredDiagnostics(t *testing.T) {
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skipf("ShellCheck required: %s", err)
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	workspace := workspaceWith(t, map[string]string{
		"my workflow.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo é 🐚 $VALUE\n",
	})
	resultPath := filepath.Join(t.TempDir(), "result.json")
	outputPath := filepath.Join(t.TempDir(), "output")
	env := map[string]string{
		"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath,
		"ACTIONLINT_ACTION_RESULT": resultPath, "ACTIONLINT_SHELLCHECK_COMMAND": shellcheck,
		"INPUT_FILES": "my workflow.yml", "INPUT_FORMAT": "sarif",
		"INPUT_OUTPUT-FILE": "report.sarif", "INPUT_PYFLAKES": "false",
	}
	var output strings.Builder
	if code := Main(func(key string) string { return env[key] }, &output); code != 1 {
		t.Fatalf("code %d; want 1: %s", code, output.String())
	}
	selected := parseOutputs(read(t, outputPath))["output"]
	if report := read(t, filepath.Join(workspace, "report.sarif")); strings.TrimSuffix(report, "\n") != selected {
		t.Fatal("output-file differs from selected output")
	}
	var persisted persistedResult
	if err := json.Unmarshal([]byte(read(t, resultPath)), &persisted); err != nil {
		t.Fatal(err)
	}
	// The persisted envelope compacts its SARIF document.
	selectedJSON, err := json.Marshal(json.RawMessage(selected))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(selectedJSON, persisted.SARIF) {
		t.Fatalf("selected SARIF differs from persisted SARIF: %s / %s", selectedJSON, persisted.SARIF)
	}
	var document struct {
		Runs []struct {
			ColumnKind string
			Results    []struct {
				Level      string
				Properties struct{ ExternalCode string }
				Fixes      []json.RawMessage
				Locations  []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI string }
						Region           struct{ StartLine, StartColumn, EndLine, EndColumn int }
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(selected), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Runs) != 1 || len(document.Runs[0].Results) != 1 {
		t.Fatalf("wanted one SARIF result: %s", selected)
	}
	run := document.Runs[0]
	finding := run.Results[0]
	if run.ColumnKind != "unicodeCodePoints" || finding.Level != "note" || finding.Properties.ExternalCode != "SC2086" || len(finding.Fixes) != 1 {
		t.Fatalf("ShellCheck metadata or fix lost: %s", selected)
	}
	if len(finding.Locations) != 1 {
		t.Fatalf("wanted one source location: %s", selected)
	}
	location := finding.Locations[0].PhysicalLocation
	if location.ArtifactLocation.URI != "my%20workflow.yml" || location.Region.StartLine != 7 || location.Region.EndLine != 7 || location.Region.StartColumn != 20 || location.Region.EndColumn != 26 {
		t.Fatalf("Unicode source range or escaped path changed: %s", selected)
	}
}

func TestPersistedResultRetainsAnalysisStatus(t *testing.T) {
	for _, tc := range []struct {
		name, content, format string
		code, analysisCode    int
		completed             bool
	}{
		{"clean", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", "json", 0, 0, true},
		{"findings without step failure", "on: push\njobs:\n  test:\n    runs-on: invalid-runner\n    steps:\n      - run: echo ok\n", "sarif", 0, 1, true},
		{"invalid format", "", "invalid", 2, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "ci.yml"), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "result.json")
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "ACTIONLINT_ACTION_RESULT": path,
				"INPUT_FILES": "ci.yml", "INPUT_FORMAT": tc.format, "INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false", "INPUT_FAIL-ON-ERROR": "false"}
			var output strings.Builder
			if got := Main(func(key string) string { return env[key] }, &output); got != tc.code {
				t.Fatalf("code %d; want %d: %s", got, tc.code, output.String())
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var result persistedResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.SchemaVersion != 1 || result.Completed != tc.completed || result.ExitCode != tc.analysisCode {
				t.Fatalf("unexpected status: %+v", result)
			}
			if tc.completed {
				if result.FileCount == nil || *result.FileCount != 1 || !json.Valid(result.SARIF) {
					t.Fatalf("missing counts or SARIF: %s", data)
				}
				if len(result.Diagnostics) != tc.analysisCode {
					t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
				}
			} else if result.Error == "" || result.FileCount != nil {
				t.Fatalf("invalid input presented as completed: %s", data)
			}
		})
	}
}

func TestPersistedResultRecordsMissingInputFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	env := map[string]string{"GITHUB_WORKSPACE": t.TempDir(), "ACTIONLINT_ACTION_RESULT": path,
		"INPUT_FILES": "missing.yml", "INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false"}
	var output strings.Builder
	if code := Main(func(key string) string { return env[key] }, &output); code != 3 {
		t.Fatalf("code %d: %s", code, output.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result persistedResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Completed || result.Status != "failure" || result.Error == "" {
		t.Fatalf("failure not retained: %s", data)
	}
}
