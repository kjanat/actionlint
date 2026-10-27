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

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/buildinfo"
	"actionlint.kjanat.dev/v2/internal/render"
)

func TestActionReportsFilesOnAnotherVolume(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("different Windows volumes have no relative path")
	}
	const workspace = `C:\workspace`
	const file = `D:\shared\workflow.yml`
	analysis := &application.Result{Diagnostics: []diagnostic.Diagnostic{{
		Rule: "custom", Path: file, Message: "Quote variable",
		Start: diagnostic.Position{Line: 1, Column: 6}, End: diagnostic.Position{Line: 1, Column: 8},
		Fixes: []diagnostic.Fix{{Description: "Quote", Edits: []diagnostic.Edit{{Path: file}}}},
	}}}
	renderer, err := render.New(render.FormatSARIF, "", false, buildinfo.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sarif bytes.Buffer
	if err := renderer.Render(&sarif, render.Prepare(*workspaceReportAnalysis(analysis, workspace, workspace))); err != nil {
		t.Fatal(err)
	}
	if got := sarif.String(); strings.Count(got, "file:///D:/shared/workflow.yml") != 2 || strings.Contains(got, "uriBaseId") {
		t.Fatalf("expected absolute file URIs for location and fix: %s", got)
	}
	if analysis.Diagnostics[0].Path != file || analysis.Diagnostics[0].Fixes[0].Edits[0].Path != file {
		t.Fatal("SARIF rendering changed persisted paths")
	}
	legacyAnalysis := analysisForRender(t, "")
	legacyAnalysis.Diagnostics[0].Path = file
	annotation, err := renderAnalysis(formatGitHub, legacyAnalysis, workspace, workspace)
	if err != nil || !strings.Contains(annotation, "file=D%3A/shared/workflow.yml,") {
		t.Fatalf("cross-volume annotation: %q %v", annotation, err)
	}
}

func TestPersistedResultRetainsAnalysisStatus(t *testing.T) {
	for _, tc := range []struct {
		name, content, format string
		code, analysisCode    int
		completed             bool
	}{
		{"clean", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`, "json", 0, 0, true},
		{"findings without step failure", `on: push
jobs:
  test:
    runs-on: invalid-runner
    steps:
      - run: echo ok
`, "sarif", 0, 1, true},
		{"invalid format", "", "invalid", 2, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "ci.yml"), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "result.json")
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "ACTIONLINT_ACTION_RESULT": path,
				"INPUT_FILES": "ci.yml", "INPUT_FORMAT": tc.format, "INPUT_FAIL-ON-ERROR": "false",
			}
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
				if result.FileCount == nil || *result.FileCount != 1 {
					t.Fatalf("missing file count: %s", data)
				}
				if (tc.format == "sarif") != json.Valid(result.SARIF) {
					t.Fatalf("unexpected SARIF presence: %s", data)
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
	env := map[string]string{
		"GITHUB_WORKSPACE": t.TempDir(), "ACTIONLINT_ACTION_RESULT": path,
		"GITHUB_OUTPUT": outputPath, "INPUT_FORMAT": "json",
		"INPUT_FILES": "missing.yml",
	}
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
		t.Fatalf("failure data missing: %s", data)
	}
	if rendered := parseOutputs(read(t, outputPath))["output"]; rendered != strings.TrimSuffix(string(data), "\n") {
		t.Fatalf("failure differs between JSON output and persisted result: %s", rendered)
	}
}

func TestActionSARIFGeneratedOnlyWhenRequested(t *testing.T) {
	for _, tc := range []struct {
		name, format, sarif string
		wantSARIF           bool
	}{
		{"default JSON", "json", "", false},
		{"disabled", "json", "false", false},
		{"exact true required", "json", "TRUE", false},
		{"separate report", "json", "true", true},
		{"console report", "sarif", "false", true},
		{"both reports", "sarif", "true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := workspaceWith(t, map[string]string{"ci.yml": cleanWorkflow})
			resultPath := filepath.Join(t.TempDir(), "result.json")
			outputPath := filepath.Join(t.TempDir(), "outputs")
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath,
				"ACTIONLINT_ACTION_RESULT": resultPath,
				"INPUT_FILES":              "ci.yml", "INPUT_FORMAT": tc.format, "INPUT_SARIF": tc.sarif,
			}
			var stdout strings.Builder
			if code := Main(func(key string) string { return env[key] }, &stdout); code != 0 {
				t.Fatalf("code %d: %s", code, &stdout)
			}
			data := read(t, resultPath)
			var result render.CheckResult
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				t.Fatal(err)
			}
			if tc.wantSARIF != json.Valid(result.SARIF) || !tc.wantSARIF && result.SARIF != nil {
				t.Fatalf("unexpected SARIF presence: %s", data)
			}
			selected := parseOutputs(read(t, outputPath))["output"]
			if tc.format == "json" {
				if selected != strings.TrimSpace(data) {
					t.Fatal("JSON output differs from persisted result")
				}
			} else {
				compacted, err := json.Marshal(json.RawMessage(selected))
				if err != nil || !bytes.Equal(compacted, result.SARIF) {
					t.Fatalf("console SARIF differs from embedded SARIF: %v", err)
				}
			}
		})
	}
}
