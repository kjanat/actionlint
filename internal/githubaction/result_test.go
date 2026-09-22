package githubaction

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
