package githubaction

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev"
	"go.yaml.in/yaml/v4"
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

func TestBuildRequestFilePaths(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{
		"workflow.yaml":     cleanWorkflow,
		"sub/workflow.yaml": cleanWorkflow,
	})
	outside := filepath.Join(filepath.Dir(workspace), "outside.yaml")
	for _, tc := range []struct {
		name       string
		workingRel string
		file       string
		wantError  bool
	}{
		{"relative from root", ".", "workflow.yaml", false},
		{"relative from subdirectory", "sub", "workflow.yaml", false},
		{"relative parent inside workspace", "sub", "../workflow.yaml", false},
		{"absolute inside workspace", ".", filepath.Join(workspace, "workflow.yaml"), false},
		{"absolute inside workspace from subdirectory", "sub", filepath.Join(workspace, "workflow.yaml"), false},
		{"relative outside workspace", ".", "../outside.yaml", true},
		{"relative outside workspace from subdirectory", "sub", "../../outside.yaml", true},
		{"absolute outside workspace", ".", outside, true},
		{"absolute outside workspace from subdirectory", "sub", outside, true},
		{"absolute workspace prefix", "sub", workspace + "-other" + string(filepath.Separator) + "workflow.yaml", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := buildRequest(&inputs{files: []string{tc.file}}, workspace, tc.workingRel)
			if tc.wantError {
				const want = "Input 'files' must stay within the repository workspace"
				if err == nil || err.Error() != want {
					t.Fatalf("wanted %q, got %v", want, err)
				}
				if req != nil {
					t.Fatal("invalid paths must not produce a lint request")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(req.files) != 1 || req.files[0] != tc.file {
				t.Errorf("wanted the original file path %q, got %#v", tc.file, req.files)
			}
		})
	}
}

func TestRunLinterFindsNoProblem(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"clean.yaml": cleanWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON, files: []string{"clean.yaml"}})
	if got.code != actionlint.ExitStatusSuccessNoProblem {
		t.Fatalf("wanted exit code 0 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	if got.stdout != "[]\n" {
		t.Errorf("wanted an empty JSON array but got %q", got.stdout)
	}
	if !got.fileCountKnown || got.fileCount != 1 {
		t.Errorf("wanted the selected file count to be 1 but got %#v", got)
	}
}

func TestRunLinterDistinguishesEmptyDiscovery(t *testing.T) {
	dir := workspaceWith(t, map[string]string{
		".git":                         "",
		".github/workflows/readme.txt": "not a workflow",
	})
	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON})

	if got.code != actionlint.ExitStatusFailure || !strings.Contains(got.stderr, "no YAML file was found") {
		t.Fatalf("wanted empty discovery to fail linting but got %#v", got)
	}
	if !got.fileCountKnown || got.fileCount != 0 {
		t.Errorf("wanted a known zero file count but got %#v", got)
	}
}

func TestRunLinterFindsProblems(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON, files: []string{"broken.yaml"}})
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

	got := runLinter(&lintRequest{workingDir: dir, format: formatSARIF, files: []string{"broken.yaml"}})
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
	if got.stdout != got.sarif {
		t.Error("selected SARIF differs from the persisted native SARIF document")
	}
}

func TestRunLinterAppliesIgnorePatterns(t *testing.T) {
	dir := workspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{
		workingDir: dir,
		format:     formatJSON,
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
		name       string
		req        *lintRequest
		want       string
		count      int
		countKnown bool
	}{
		{
			"missing file",
			&lintRequest{workingDir: dir, format: formatJSON, files: []string{"missing.yaml"}},
			"could not read",
			1,
			true,
		},
		{
			"invalid ignore pattern",
			&lintRequest{workingDir: dir, format: formatJSON, files: []string{"clean.yaml"}, ignore: []string{"("}},
			"invalid regular expression",
			0,
			false,
		},
		{
			"missing config file",
			&lintRequest{workingDir: dir, format: formatJSON, files: []string{"clean.yaml"}, configFile: filepath.Join(dir, "none.yaml")},
			"could not read config file",
			0,
			false,
		},
		{
			"no repository",
			&lintRequest{workingDir: dir, format: formatJSON},
			"no project was found",
			0,
			false,
		},
	} {
		got := runLinter(tc.req)
		if got.code != actionlint.ExitStatusFailure {
			t.Errorf("%s: wanted exit code 3 but got %d", tc.name, got.code)
		}
		if !strings.Contains(got.stderr, tc.want) {
			t.Errorf("%s: wanted %q in %q", tc.name, tc.want, got.stderr)
		}
		if got.fileCountKnown != tc.countKnown || got.fileCount != tc.count {
			t.Errorf("%s: wanted file count %d (known %t) but got %#v", tc.name, tc.count, tc.countKnown, got)
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

	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON})
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
	if !got.fileCountKnown || got.fileCount != 2 {
		t.Errorf("wanted the two linted repository files counted but got %#v", got)
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
		format:     formatJSON,
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
	status := fmt.Sprintf(
		"actionlint %s: 1 problem in 1 workflow file (external linters disabled)\n",
		actionVersion(),
	)
	if !strings.HasPrefix(out.String(), status+"::stop-commands::DELIM\n"+want) {
		t.Errorf("wanted the status and output wrapped in stop commands but got %q", out.String())
	}
}

func TestActionIgnoreAndAutomaticConfigEndToEnd(t *testing.T) {
	for _, tc := range []struct {
		name       string
		inputYAML  string
		configName string
		wantCode   int
	}{
		{"no ignore", "ignore: ''\n", "", 1},
		{"quoted block pattern", "ignore: |\n  'label \"ubuntu-24.04-custom\" is unknown.'\n", "", 1},
		{"block pattern", "ignore: |\n  label \"ubuntu-24.04-custom\" is unknown\\.\n", "", 0},
		{"quoted scalar pattern", "ignore: 'label \"ubuntu-24.04-custom\" is unknown\\.'\n", "", 0},
		{"automatic yaml config", "ignore: ''\n", "actionlint.yaml", 0},
		{"automatic yml config", "ignore: ''\n", "actionlint.yml", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in struct {
				Ignore string `yaml:"ignore"`
			}
			if err := yaml.Unmarshal([]byte(tc.inputYAML), &in); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{
				".git":                        "",
				".github/workflows/test.yaml": strings.ReplaceAll(brokenWorkflow, "unknown-runner", "ubuntu-24.04-custom"),
			}
			if tc.configName != "" {
				files[".github/"+tc.configName] = "self-hosted-runner:\n  labels:\n    - ubuntu-24.04-custom\n"
			}
			workspace := workspaceWith(t, files)
			outputPath := filepath.Join(t.TempDir(), "output")
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath}
			var out strings.Builder
			a := &action{
				args:    args("", "github", in.Ignore, "", "false", "false", ".", "", "true"),
				stdout:  &out,
				env:     func(name string) string { return env[name] },
				lint:    runLinter,
				newID:   fixedID("DELIM"),
				timeout: lintTimeout,
			}
			if code := a.run(); code != tc.wantCode {
				t.Fatalf("wanted exit code %d but got %d: %s", tc.wantCode, code, out.String())
			}
			outputs := parseOutputs(read(t, outputPath))
			if want := strconv.Itoa(tc.wantCode); outputs["problem-count"] != want {
				t.Errorf("wanted %s problems but got %#v", want, outputs)
			}
			if tc.wantCode == 1 && !strings.Contains(outputs["output"], `label "ubuntu-24.04-custom" is unknown.`) {
				t.Errorf("wanted the unknown runner diagnostic but got %q", outputs["output"])
			}
		})
	}
}

func TestActionRunLintPreservesProcessDirectory(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaceWith(t, map[string]string{"sub/broken.yaml": brokenWorkflow})
	sub := filepath.Join(workspace, "sub")

	var seen string
	a := &action{
		timeout: time.Minute,
		lint: func(req *lintRequest) *lintResult {
			wd, err := os.Getwd()
			if err != nil {
				t.Error(err)
			}
			seen = wd
			return runLinter(req)
		},
	}
	got := a.runLint(&lintRequest{workingDir: sub, files: []string{"broken.yaml"}, format: formatJSON})

	if resolved(t, seen) != resolved(t, dir) {
		t.Errorf("wanted the process directory to remain %q while linting but got %q", dir, seen)
	}
	if got.code != actionlint.ExitStatusSuccessProblemFound {
		t.Fatalf("wanted the workflow in the requested directory linted but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	var problems []*problem
	if err := json.Unmarshal([]byte(got.stdout), &problems); err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Filepath != "broken.yaml" {
		t.Errorf("wanted the diagnostic path relative to the requested directory but got %#v", problems)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if resolved(t, after) != resolved(t, dir) {
		t.Errorf("wanted the process directory to remain %q after linting but got %q", dir, after)
	}
}

func TestActionRunLintTimesOutNoncooperativeLinter(t *testing.T) {
	directory := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	returned := make(chan *lintResult, 1)
	done := make(chan struct{})
	a := &action{
		timeout: 10 * time.Millisecond,
		lint: func(req *lintRequest) *lintResult {
			defer close(finished)
			close(started)
			<-release
			if req.ctx.Err() == nil {
				t.Error("wanted the timed-out analysis context canceled")
			}
			return knownFiles(&lintOutcome{"late output", "late log", actionlint.ExitStatusSuccessNoProblem}, 1)
		},
	}
	go func() {
		defer close(done)
		returned <- a.runLint(&lintRequest{workingDir: directory})
	}()
	<-started
	defer func() {
		close(release)
		<-finished
		<-done
	}()

	select {
	case got := <-returned:
		if got.code != actionlint.ExitStatusFailure || !strings.Contains(got.stderr, "actionlint timed out after") {
			t.Errorf("wanted an independent timeout result but got %#v", got)
		}
		if got.stdout != "" || got.fileCountKnown {
			t.Errorf("timeout result must not expose the unfinished analysis: %#v", got)
		}
	case <-time.After(time.Second):
		t.Error("timeout waited for the noncooperative linter to return")
	}
}

func TestActionRunLintReportsUnusableWorkingDirectory(t *testing.T) {
	a := &action{timeout: time.Minute, lint: func(*lintRequest) *lintResult {
		t.Error("actionlint must not run when the working directory is unusable")
		return nil
	}}
	workspace := workspaceWith(t, map[string]string{"file": "not a directory"})
	for _, path := range []string{"missing", "file"} {
		got := a.runLint(&lintRequest{workingDir: filepath.Join(workspace, path)})
		if got.code != actionlint.ExitStatusFailure || got.stderr == "" {
			t.Errorf("wanted a failure for %q but got %#v", path, got)
		}
	}
}
