package githubaction

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"actionlint.kjanat.dev/internal/cli"
	"github.com/google/go-cmp/cmp"
)

func TestActionMultiProjectResultParity(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{
		"z/.git":                       "",
		"z/.github/workflows/test.yml": cleanWorkflow,
		"z/.github/actionlint.yaml":    "config-variables: [Z]\n",
		"a/.git":                       "",
		"a/.github/workflows/test.yml": cleanWorkflow,
		"a/.github/actionlint.yaml":    "config-variables: [A]\n",
	})
	resultPath := filepath.Join(workspace, "result.json")
	outputPath := filepath.Join(workspace, "outputs")
	env := map[string]string{
		"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath,
		"ACTIONLINT_ACTION_RESULT": resultPath,
		"INPUT_FILES":              "z/.github/workflows/test.yml\na/.github/workflows/test.yml",
		"INPUT_FORMAT":             "json", "INPUT_OUTPUT-FILE": "report.json",
		"INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false",
	}
	var stdout bytes.Buffer
	if code := Main(func(key string) string { return env[key] }, &stdout); code != 0 {
		t.Fatalf("Action status %d: %s", code, &stdout)
	}
	rendered := parseOutputs(read(t, outputPath))["output"]
	for _, path := range []string{resultPath, filepath.Join(workspace, "report.json")} {
		if got := strings.TrimSpace(read(t, path)); got != strings.TrimSpace(rendered) {
			t.Errorf("%s differs from Action JSON output", filepath.Base(path))
		}
	}
	var result actionlint.CheckResult
	if err := json.Unmarshal([]byte(rendered), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Configs) != 2 {
		t.Fatalf("want two project configurations, got %d", len(result.Configs))
	}
	for i, name := range []string{"z", "a"} {
		if got := result.Configs[i].Project; got != filepath.Join(workspace, name) {
			t.Errorf("configuration %d project = %q", i, got)
		}
	}
	first := strings.Index(stdout.String(), "Configuration: a/.github/actionlint.yaml")
	second := strings.Index(stdout.String(), "Configuration: z/.github/actionlint.yaml")
	if first < 0 || second <= first {
		t.Fatalf("configuration feedback must remain sorted: %s", &stdout)
	}
}

func TestCLIAndActionResultContract(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		code         int
	}{
		{"clean", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", 0},
		{"findings", "on: push\njobs:\n  test:\n    runs-on: invalid-runner\n    steps:\n      - run: echo ok\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			t.Chdir(workspace)
			if err := os.WriteFile("ci.yml", []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			command := cli.Command{Stdout: &stdout, Stderr: &stderr}
			if code := command.Main([]string{"actionlint", "check", "--json", "--shellcheck=", "--pyflakes=", "ci.yml"}); code != tc.code {
				t.Fatalf("CLI status %d: %s", code, &stderr)
			}
			var cliResult actionlint.CheckResult
			if err := json.Unmarshal(stdout.Bytes(), &cliResult); err != nil {
				t.Fatal(err)
			}
			resultPath := filepath.Join(workspace, "result.json")
			outputPath := filepath.Join(workspace, "outputs")
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath, "ACTIONLINT_ACTION_RESULT": resultPath,
				"INPUT_FILES": "ci.yml", "INPUT_FORMAT": "json", "INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false", "INPUT_FAIL-ON-ERROR": "false"}
			stdout.Reset()
			if code := Main(func(key string) string { return env[key] }, &stdout); code != 0 {
				t.Fatalf("Action status %d: %s", code, &stdout)
			}
			var persisted, rendered actionlint.CheckResult
			if err := json.Unmarshal([]byte(read(t, resultPath)), &persisted); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(parseOutputs(read(t, outputPath))["output"]), &rendered); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(persisted, rendered); diff != "" {
				t.Fatalf("public and persisted JSON differ: %s", diff)
			}
			// The Action additionally supplies SARIF for its reporters.
			persisted.SARIF = nil
			if diff := cmp.Diff(cliResult, persisted); diff != "" {
				t.Fatalf("CLI and Action differ: %s", diff)
			}
			if persisted.ExitCode != tc.code {
				t.Fatal("advisory step changed analysis status")
			}
		})
	}
}
