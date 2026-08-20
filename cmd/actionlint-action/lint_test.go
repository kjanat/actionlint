package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev"
)

const cleanWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`

const brokenWorkflow = `on: push
jobs:
  test:
    runs-on: unknown-runner
    steps:
      - run: echo hi
`

func workspaceWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := resolved(t, t.TempDir())
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunLinterFindsNoProblem(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"clean.yaml": cleanWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: "{{json .}}", files: []string{"clean.yaml"}})
	if got.code != actionlint.ExitStatusSuccessNoProblem {
		t.Fatalf("wanted exit code 0 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	if got.stdout != "[]\n" {
		t.Errorf("wanted an empty JSON array but got %q", got.stdout)
	}
}

func TestRunLinterFindsProblems(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: "{{json .}}", files: []string{"broken.yaml"}})
	if got.code != actionlint.ExitStatusSuccessProblemFound {
		t.Fatalf("wanted exit code 1 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}

	var problems []*problem
	if err := json.Unmarshal([]byte(got.stdout), &problems); err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("wanted one problem but got %#v", problems)
	}
	if problems[0].Filepath != "broken.yaml" {
		t.Errorf("wanted a path relative to the working directory but got %q", problems[0].Filepath)
	}
	if problems[0].Kind != "runner-label" || problems[0].Line != 4 {
		t.Errorf("wanted the unknown runner label reported at line 4 but got %#v", problems[0])
	}
	if problems[0].Snippet == "" || problems[0].EndColumn == 0 {
		t.Errorf("wanted a snippet and an end column but got %#v", problems[0])
	}
}

func TestRunLinterRendersSARIF(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: sarifTemplate, files: []string{"broken.yaml"}})
	if got.code != actionlint.ExitStatusSuccessProblemFound {
		t.Fatalf("wanted exit code 1 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	count, err := sarifProblemCount(got.stdout)
	if err != nil {
		t.Fatalf("%v: %s", err, got.stdout)
	}
	if count != 1 {
		t.Errorf("wanted one SARIF result but got %d", count)
	}
}

func TestRunLinterAppliesIgnorePatterns(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{
		workingDir: dir,
		format:     "{{json .}}",
		files:      []string{"broken.yaml"},
		ignore:     []string{`label "unknown-runner" is unknown`},
	})
	if got.code != actionlint.ExitStatusSuccessNoProblem {
		t.Fatalf("wanted the problem ignored but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
}

func TestRunLinterReportsFatalErrors(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"clean.yaml": cleanWorkflow})
	t.Chdir(dir)

	for _, tc := range []struct {
		name string
		req  *lintRequest
		want string
	}{
		{
			"missing file",
			&lintRequest{workingDir: dir, format: "{{json .}}", files: []string{"missing.yaml"}},
			"could not read",
		},
		{
			"invalid ignore pattern",
			&lintRequest{workingDir: dir, format: "{{json .}}", files: []string{"clean.yaml"}, ignore: []string{"("}},
			"invalid regular expression",
		},
		{
			"missing config file",
			&lintRequest{workingDir: dir, format: "{{json .}}", files: []string{"clean.yaml"}, configFile: filepath.Join(dir, "none.yaml")},
			"could not read config file",
		},
		{
			"no repository",
			&lintRequest{workingDir: dir, format: "{{json .}}"},
			"no project was found",
		},
	} {
		got := runLinter(tc.req)
		if got.code != actionlint.ExitStatusFailure {
			t.Errorf("%s: wanted exit code 3 but got %d", tc.name, got.code)
		}
		if !strings.Contains(got.stderr, tc.want) {
			t.Errorf("%s: wanted %q in %q", tc.name, tc.want, got.stderr)
		}
	}
}

func TestRunLinterLintsWholeRepository(t *testing.T) {
	dir := workspaceWith(t, map[string]string{
		".git":                          "",
		".github/workflows/broken.yaml": brokenWorkflow,
		".github/workflows/clean.yaml":  cleanWorkflow,
	})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: "{{json .}}"})
	if got.code != actionlint.ExitStatusSuccessProblemFound {
		t.Fatalf("wanted exit code 1 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	var problems []*problem
	if err := json.Unmarshal([]byte(got.stdout), &problems); err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("wanted one problem but got %#v", problems)
	}
	if want := filepath.Join(".github", "workflows", "broken.yaml"); problems[0].Filepath != want {
		t.Errorf("wanted %q but got %q", want, problems[0].Filepath)
	}
}

func TestRunLinterReadsConfigFile(t *testing.T) {
	dir := workspaceWith(t, map[string]string{
		"broken.yaml": brokenWorkflow,
		"conf.yaml":   "self-hosted-runner:\n  labels:\n    - unknown-runner\n",
	})
	t.Chdir(dir)

	got := runLinter(&lintRequest{
		workingDir: dir,
		format:     "{{json .}}",
		files:      []string{"broken.yaml"},
		configFile: filepath.Join(dir, "conf.yaml"),
	})
	if got.code != actionlint.ExitStatusSuccessNoProblem {
		t.Fatalf("wanted the configured label accepted but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
}

func TestActionLintsWorkflowEndToEnd(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{"workflows/broken.yaml": brokenWorkflow})
	outputPath := filepath.Join(t.TempDir(), "output")
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath}
	var out strings.Builder

	a := &action{
		args:    args("workflows/broken.yaml", "default", "", "", "false", "false", ".", "results/out.txt", "true"),
		stdout:  &out,
		env:     func(name string) string { return env[name] },
		lint:    runLinter,
		newID:   fixedID("DELIM"),
		timeout: lintTimeout,
	}
	if code := a.run(); code != 1 {
		t.Fatalf("wanted exit code 1 but got %d: %s", code, out.String())
	}

	outputs := parseOutputs(read(t, outputPath))
	if outputs["result"] != "problems-found" || outputs["problem-count"] != "1" {
		t.Errorf("wanted one problem but got %#v", outputs)
	}
	if outputs["output-file"] != "results/out.txt" {
		t.Errorf("wanted the written file reported but got %q", outputs["output-file"])
	}
	want := "workflows/broken.yaml:4:14: label \"unknown-runner\" is unknown."
	if !strings.HasPrefix(outputs["output"], want) {
		t.Errorf("wanted the default format output but got %q", outputs["output"])
	}
	if content := read(t, filepath.Join(workspace, "results", "out.txt")); !strings.HasPrefix(content, want) {
		t.Errorf("wanted the same content in the output file but got %q", content)
	}
	if !strings.HasPrefix(out.String(), "::stop-commands::DELIM\n"+want) {
		t.Errorf("wanted the output wrapped in stop commands but got %q", out.String())
	}
}

func TestActionRunLintRestoresProcessDirectory(t *testing.T) {
	dir := resolved(t, t.TempDir())
	t.Chdir(dir)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	var seen string
	a := &action{
		timeout: time.Minute,
		lint: func(*lintRequest) *lintOutcome {
			wd, err := os.Getwd()
			if err != nil {
				t.Error(err)
			}
			seen = wd
			return &lintOutcome{"[]\n", "", actionlint.ExitStatusSuccessNoProblem}
		},
	}
	a.runLint(&lintRequest{workingDir: sub})

	if resolved(t, seen) != sub {
		t.Errorf("wanted actionlint to run in %q but it ran in %q", sub, seen)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if resolved(t, after) != dir {
		t.Errorf("wanted the process directory restored to %q but got %q", dir, after)
	}
}

func TestActionRunLintReportsUnusableWorkingDirectory(t *testing.T) {
	a := &action{timeout: time.Minute, lint: func(*lintRequest) *lintOutcome {
		t.Error("actionlint must not run when the working directory is unusable")
		return nil
	}}
	got := a.runLint(&lintRequest{workingDir: filepath.Join(t.TempDir(), "missing")})
	if got.code != actionlint.ExitStatusFailure || got.stderr == "" {
		t.Errorf("wanted a failure but got %#v", got)
	}
}
