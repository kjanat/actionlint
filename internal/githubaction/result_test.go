package githubaction

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestActionReportsFilesOnAnotherVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("different Windows volumes have no relative path")
	}
	const workspace = `C:\workspace`
	const file = `D:\shared\workflow.yml`
	analysis := &actionlint.AnalysisResult{Diagnostics: []actionlint.Diagnostic{{
		Rule: "shellcheck", Path: file, Message: "Quote variable",
		Start: actionlint.DiagnosticPosition{Line: 1, Column: 6}, End: actionlint.DiagnosticPosition{Line: 1, Column: 8},
		Fixes: []actionlint.DiagnosticFix{{Description: "Quote", Edits: []actionlint.DiagnosticEdit{{Path: file}}}},
	}}}
	renderer, err := actionlint.NewAnalysisRenderer(actionlint.OutputFormatSARIF, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var sarif bytes.Buffer
	if err := renderer.Render(&sarif, workspaceSARIFAnalysis(analysis, workspace, workspace)); err != nil {
		t.Fatal(err)
	}
	if got := sarif.String(); strings.Count(got, "file:///D:/shared/workflow.yml") != 2 || strings.Contains(got, "uriBaseId") {
		t.Fatalf("expected absolute file URIs for location and fix: %s", got)
	}
	if analysis.Diagnostics[0].Path != file || analysis.Diagnostics[0].Fixes[0].Edits[0].Path != file {
		t.Fatal("SARIF rendering changed persisted paths")
	}
	problem := sampleProblem()
	problem.Filepath = file
	serialized, err := json.Marshal([]*actionlint.ErrorTemplateFields{problem})
	if err != nil {
		t.Fatal(err)
	}
	count, annotation, err := countAndRender(string(serialized), formatGitHub, workspace, workspace)
	if err != nil || count != 1 || !strings.Contains(annotation, "file=D%3A/shared/workflow.yml,") {
		t.Fatalf("cross-volume annotation: %d %q %v", count, annotation, err)
	}
}

func TestActionDiagnosticAndSARIFPaths(t *testing.T) {
	shellcheck, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skipf("ShellCheck required: %s", err)
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          echo $VALUE\n"
	workspace := workspaceWith(t, map[string]string{
		".git": "", "my workflow.yml": workflow, "work/my workflow.yml": workflow, ".github/workflows/my workflow.yml": workflow,
	})
	for _, format := range []string{"default", "json", "sarif"} {
		for _, tc := range []struct{ name, file, diagnostic, uri string }{
			{"relative spelling", "./my workflow.yml", "./my workflow.yml", "work/my%20workflow.yml"},
			{"parent path", "../my workflow.yml", "../my workflow.yml", "my%20workflow.yml"},
			{"absolute input", filepath.Join(workspace, "my workflow.yml"), "../my workflow.yml", "my%20workflow.yml"},
			{"repository discovery", "", "../.github/workflows/my workflow.yml", ".github/workflows/my%20workflow.yml"},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				resultPath := filepath.Join(t.TempDir(), "result.json")
				outputPath := filepath.Join(t.TempDir(), "output")
				env := map[string]string{
					"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath, "ACTIONLINT_ACTION_RESULT": resultPath,
					"ACTIONLINT_SHELLCHECK_COMMAND": shellcheck, "INPUT_PYFLAKES": "false", "INPUT_WORKING-DIRECTORY": "work",
					"INPUT_FILES": tc.file, "INPUT_FORMAT": format, "INPUT_OUTPUT-FILE": "report.txt",
				}
				var output strings.Builder
				if code := Main(func(key string) string { return env[key] }, &output); code != 1 {
					t.Fatalf("code %d; want findings: %s", code, output.String())
				}
				var result persistedResult
				if err := json.Unmarshal([]byte(read(t, resultPath)), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Diagnostics) != 1 || len(result.Diagnostics[0].Fixes) != 1 {
					t.Fatalf("wanted one fixable ShellCheck diagnostic: %+v", result.Diagnostics)
				}
				diagnostic := result.Diagnostics[0]
				if filepath.ToSlash(diagnostic.Path) != tc.diagnostic {
					t.Errorf("diagnostic spelling = %q, want %q", diagnostic.Path, tc.diagnostic)
				}
				for _, edit := range diagnostic.Fixes[0].Edits {
					if edit.Path != diagnostic.Path {
						t.Errorf("same-file fix path %q differs from diagnostic %q", edit.Path, diagnostic.Path)
					}
				}
				var document struct {
					Runs []struct {
						Tool struct {
							Driver struct{ Rules []struct{ ID string } }
						}
						Results []struct {
							Locations []struct {
								PhysicalLocation struct {
									ArtifactLocation struct{ URI, URIBaseID string }
								}
							}
							Fixes []struct {
								ArtifactChanges []struct {
									ArtifactLocation struct{ URI, URIBaseID string }
								}
							}
						}
					}
				}
				if err := json.Unmarshal(result.SARIF, &document); err != nil {
					t.Fatal(err)
				}
				if len(document.Runs) != 1 || len(document.Runs[0].Results) != 1 {
					t.Fatalf("wanted one SARIF finding: %s", result.SARIF)
				}
				run := document.Runs[0]
				if !slices.ContainsFunc(run.Tool.Driver.Rules, func(rule struct{ ID string }) bool { return rule.ID == "shellcheck" }) {
					t.Error("SARIF lost private rule metadata")
				}
				finding := run.Results[0]
				if len(finding.Locations) != 1 || len(finding.Fixes) != 1 || len(finding.Fixes[0].ArtifactChanges) != 1 {
					t.Fatalf("SARIF location or fix lost: %s", result.SARIF)
				}
				for _, artifact := range []struct{ URI, URIBaseID string }{
					finding.Locations[0].PhysicalLocation.ArtifactLocation, finding.Fixes[0].ArtifactChanges[0].ArtifactLocation,
				} {
					if artifact.URI != tc.uri || artifact.URIBaseID != "%SRCROOT%" {
						t.Errorf("SARIF artifact = %+v, want workspace-relative %q", artifact, tc.uri)
					}
				}
				selected := parseOutputs(read(t, outputPath))["output"]
				if file := read(t, filepath.Join(workspace, "report.txt")); strings.TrimSuffix(file, "\n") != selected {
					t.Error("output-file differs from selected output")
				}
				if format == "sarif" {
					compacted, err := json.Marshal(json.RawMessage(selected))
					if err != nil || !bytes.Equal(compacted, result.SARIF) {
						t.Errorf("selected and persisted SARIF differ: %v", err)
					}
				}
			})
		}
	}
}

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
	outputPath := filepath.Join(t.TempDir(), "outputs")
	env := map[string]string{"GITHUB_WORKSPACE": t.TempDir(), "ACTIONLINT_ACTION_RESULT": path,
		"GITHUB_OUTPUT": outputPath, "INPUT_FORMAT": "json",
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
	if rendered := parseOutputs(read(t, outputPath))["output"]; rendered != strings.TrimSuffix(string(data), "\n") {
		t.Fatalf("failure differs between JSON output and persisted result: %s", rendered)
	}
}
