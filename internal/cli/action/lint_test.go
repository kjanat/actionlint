package githubaction

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"

	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"github.com/kjanat/go-yaml/v4"
)

func TestOptionalNativeShellcheckWithoutExecutable(t *testing.T) {
	workspace := nativeWorkspaceWith(t, map[string]string{".git": "", "ci.yml": cleanWorkflow, ".github/actionlint.yaml": "plugins: []\n"})
	t.Setenv("PATH", t.TempDir())
	req := &lintRequest{workingDir: workspace, files: []string{"ci.yml"}, format: formatJSON}
	if result := runLinter(req); result.code != 0 {
		t.Fatalf("optional native ShellCheck was required: %s", result.stderr)
	}
	req.processes = map[string]*processconfig.Options{"shellcheck": {Executable: new("missing-explicit-shellcheck")}}
	if result := runLinter(req); result.code != commandstatus.ExitFailure || !strings.Contains(result.stderr, "missing-explicit-shellcheck") {
		t.Fatalf("explicit missing executable was ignored: code=%d %s", result.code, result.stderr)
	}
}

const cleanWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
permissions: {}
`

const brokenWorkflow = `on: push
jobs:
  test:
    runs-on: unknown-runner
    steps:
      - run: echo hi
permissions: {}
`

func knownRunnerOverlay(t *testing.T) repoconfig.Overlay {
	t.Helper()
	overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte("repository: {runners: []}"))
	if err != nil {
		t.Fatal(err)
	}
	return overlay
}

func nativeWorkspaceWith(t *testing.T, files map[string]string) string {
	t.Helper()
	files = maps.Clone(files)
	if _, repository := files[".git"]; !repository {
		files[".git"] = ""
	}
	if _, configured := files[".github/actionlint.yaml"]; !configured {
		files[".github/actionlint.yaml"] = "plugins: []\n"
	}
	return workspaceWith(t, files)
}

func workspaceWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := resolved(t, t.TempDir())
	for name, content := range files {
		if name == ".git" {
			command := exec.CommandContext(t.Context(), "git", "init", "--quiet", dir)
			for _, entry := range os.Environ() {
				if name, _, _ := strings.Cut(entry, "="); !strings.HasPrefix(name, "GIT_") {
					command.Env = append(command.Env, entry)
				}
			}
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, out)
			}
			continue
		}
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
	t.Parallel()
	workspace := nativeWorkspaceWith(t, map[string]string{
		"workflow.yaml":     cleanWorkflow,
		"sub/workflow.yaml": cleanWorkflow,
	})
	outside := filepath.Join(filepath.Dir(workspace), "outside.yaml")
	for _, tc := range []struct {
		name       string
		workingRel string
		file       string
	}{
		{"relative from root", ".", "workflow.yaml"},
		{"relative from subdirectory", "sub", "workflow.yaml"},
		{"relative parent inside workspace", "sub", "../workflow.yaml"},
		{"absolute inside workspace", ".", filepath.Join(workspace, "workflow.yaml")},
		{"absolute inside workspace from subdirectory", "sub", filepath.Join(workspace, "workflow.yaml")},
		{"relative outside workspace", ".", "../outside.yaml"},
		{"relative outside workspace from subdirectory", "sub", "../../outside.yaml"},
		{"absolute outside workspace", ".", outside},
		{"absolute outside workspace from subdirectory", "sub", outside},
		{"absolute workspace prefix", "sub", workspace + "-other" + string(filepath.Separator) + "workflow.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := buildRequest(&inputs{files: []string{tc.file}}, workspace, tc.workingRel)
			if len(req.files) != 1 || req.files[0] != tc.file {
				t.Errorf("wanted the original file path %q, got %#v", tc.file, req.files)
			}
		})
	}
}

func TestRunLinterWorkflowSymlinks(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"discovery", "explicit", "subdirectory"} {
		for _, contained := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/contained=%t", mode, contained), func(t *testing.T) {
				t.Parallel()
				workspace := nativeWorkspaceWith(t, map[string]string{
					".git":                         "",
					"fixtures/clean.yaml":          cleanWorkflow,
					".github/workflows/readme.txt": "",
					"sub/readme.txt":               "",
				})
				target := filepath.Join(workspace, "fixtures", "clean.yaml")
				if !contained {
					target = filepath.Join(nativeWorkspaceWith(t, map[string]string{"clean.yaml": cleanWorkflow}), "clean.yaml")
				}
				link := filepath.Join(workspace, ".github", "workflows", "linked.yaml")
				rel, err := filepath.Rel(filepath.Dir(link), target)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(rel, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				in := &inputs{format: formatJSON}
				workingRel := "."
				switch mode {
				case "explicit":
					in.files = []string{link}
				case "subdirectory":
					workingRel = "sub"
					in.files = []string{"../.github/workflows/linked.yaml"}
				}
				req := buildRequest(in, workspace, workingRel)
				got := runLinter(req)
				if got.code != commandstatus.ExitSuccessNoProblem || got.selectedFileCount() != 1 {
					t.Fatalf("linked workflow failed: %#v, %s", got, got.stderr)
				}
			})
		}
	}
}

func TestRunLinterFindsNoProblem(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{"clean.yaml": cleanWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON, files: []string{"clean.yaml"}})
	if got.code != commandstatus.ExitSuccessNoProblem {
		t.Fatalf("wanted exit code 0 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	if len(got.report.Diagnostics) != 0 || got.stdout != "" {
		t.Errorf("wanted empty typed diagnostics without intermediate JSON: %#v", got)
	}
	if got.report.FileCount == nil || got.selectedFileCount() != 1 {
		t.Errorf("wanted the selected file count to be 1 but got %#v", got)
	}
}

func TestRunLinterDistinguishesEmptyDiscovery(t *testing.T) {
	t.Parallel()
	dir := nativeWorkspaceWith(t, map[string]string{
		".git":                         "",
		".github/workflows/readme.txt": "not a workflow",
	})
	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON})

	if got.code != commandstatus.ExitFailure || !strings.Contains(got.stderr, "no workflow or action files were found") {
		t.Fatalf("wanted empty discovery to fail linting; code=%d stderr=%q", got.code, got.stderr)
	}
	if got.report.FileCount == nil || got.selectedFileCount() != 0 {
		t.Errorf("wanted a known zero file count but got %#v", got)
	}
}

func TestRunLinterCountsIncludedFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		excluded string
		files    []string
		count    int
	}{
		{"discovered subset", ".github/workflows/skip.yaml", []string{".github/workflows/*.yaml"}, 1},
		{"all discovered files excluded", ".github/workflows/*.yaml", []string{".github/workflows/*.yaml"}, 0},
		{"literal files override exclusions", ".github/workflows/*.yaml", []string{".github/workflows/skip.yaml", ".github/workflows/keep.yaml"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := nativeWorkspaceWith(t, map[string]string{
				".github/workflows/skip.yaml": brokenWorkflow,
				".github/workflows/keep.yaml": cleanWorkflow,
				"config.yml":                  "plugins: []\nfiles: {excludes: ['" + tc.excluded + "']}\n",
			})
			got := runLinter(&lintRequest{workingDir: dir, format: formatJSON, files: tc.files, configFile: filepath.Join(dir, "config.yml")})
			if got.code != commandstatus.ExitSuccessNoProblem || got.report.FileCount == nil || got.selectedFileCount() != tc.count || len(got.report.Diagnostics) != 0 {
				t.Fatalf("included-file count=%d known=%v exit=%d diagnostics=%+v stderr=%s", got.selectedFileCount(), (got.report.FileCount != nil), got.code, got.report.Diagnostics, got.stderr)
			}
		})
	}
}

func TestRunLinterFindsProblems(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON, files: []string{"broken.yaml"}, overlays: []repoconfig.Overlay{knownRunnerOverlay(t)}})
	if got.code != commandstatus.ExitSuccessProblemFound {
		t.Fatalf("wanted exit code 1 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}

	problems := got.report.Diagnostics
	if len(problems) != 1 {
		t.Fatalf("wanted one problem but got %#v", problems)
	}
	if problems[0].Path != "broken.yaml" {
		t.Errorf("wanted a path relative to the working directory but got %q", problems[0].Path)
	}
	if problems[0].Rule != "runner-label" || problems[0].Start.Line != 4 {
		t.Errorf("wanted the unknown runner label reported at line 4 but got %#v", problems[0])
	}
	if problems[0].Snippet == "" || problems[0].End.Column == 0 {
		t.Errorf("wanted a snippet and an end column but got %#v", problems[0])
	}
}

func TestRunLinterRendersSARIF(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: formatSARIF, files: []string{"broken.yaml"}, overlays: []repoconfig.Overlay{knownRunnerOverlay(t)}})
	if got.code != commandstatus.ExitSuccessProblemFound {
		t.Fatalf("wanted exit code 1 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	var document struct {
		Runs []struct{ Results []json.RawMessage }
	}
	if err := json.Unmarshal([]byte(got.stdout), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Runs) != 1 || len(document.Runs[0].Results) != 1 {
		t.Errorf("wanted one SARIF result: %s", got.stdout)
	}
	if got.stdout != string(got.report.SARIF) {
		t.Error("selected SARIF differs from the persisted native SARIF document")
	}
}

func TestRunLinterAppliesIgnorePatterns(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{"broken.yaml": brokenWorkflow})
	t.Chdir(dir)

	got := runLinter(&lintRequest{
		workingDir: dir,
		format:     formatJSON,
		files:      []string{"broken.yaml"},
		ignore:     []string{`label "unknown-runner" is unknown`},
		overlays:   []repoconfig.Overlay{knownRunnerOverlay(t)},
	})
	if got.code != commandstatus.ExitSuccessNoProblem {
		t.Fatalf("wanted the problem ignored but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
}

func TestRunLinterReportsFatalErrors(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{"clean.yaml": cleanWorkflow})
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
			&lintRequest{workingDir: t.TempDir(), format: formatJSON},
			"no project was found",
			0,
			false,
		},
	} {
		got := runLinter(tc.req)
		if got.code != commandstatus.ExitFailure {
			t.Errorf("%s: wanted exit code 3 but got %d", tc.name, got.code)
		}
		if !strings.Contains(got.stderr, tc.want) {
			t.Errorf("%s: wanted %q in %q", tc.name, tc.want, got.stderr)
		}
		if (got.report.FileCount != nil) != tc.countKnown || got.selectedFileCount() != tc.count {
			t.Errorf("%s: wanted file count %d (known %t) but got %#v", tc.name, tc.count, tc.countKnown, got)
		}
	}
}

func TestRunLinterLintsWholeRepository(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{
		".git":                          "",
		".github/workflows/broken.yaml": brokenWorkflow,
		".github/workflows/clean.yaml":  cleanWorkflow,
	})
	t.Chdir(dir)

	got := runLinter(&lintRequest{workingDir: dir, format: formatJSON, overlays: []repoconfig.Overlay{knownRunnerOverlay(t)}})
	if got.code != commandstatus.ExitSuccessProblemFound {
		t.Fatalf("wanted exit code 1 but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	problems := got.report.Diagnostics
	if len(problems) != 1 {
		t.Fatalf("wanted one problem but got %#v", problems)
	}
	if got.report.FileCount == nil || got.selectedFileCount() != 2 {
		t.Errorf("wanted the two linted repository files counted but got %#v", got)
	}
	if want := filepath.Join(".github", "workflows", "broken.yaml"); problems[0].Path != want {
		t.Errorf("wanted %q but got %q", want, problems[0].Path)
	}
}

func TestRunLinterReadsConfigFile(t *testing.T) {
	dir := nativeWorkspaceWith(t, map[string]string{
		"broken.yaml": brokenWorkflow,
		"conf.yaml": `plugins: []
self-hosted-runner:
  labels:
    - unknown-runner
`,
	})
	t.Chdir(dir)

	got := runLinter(&lintRequest{
		workingDir: dir,
		format:     formatJSON,
		files:      []string{"broken.yaml"},
		configFile: filepath.Join(dir, "conf.yaml"),
	})
	if got.code != commandstatus.ExitSuccessNoProblem {
		t.Fatalf("wanted the configured label accepted but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
}

func TestActionLintsWorkflowEndToEnd(t *testing.T) {
	t.Parallel()
	workspace := nativeWorkspaceWith(t, map[string]string{"workflows/broken.yaml": brokenWorkflow})
	outputPath := filepath.Join(t.TempDir(), "output")
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath, "INPUT_DISABLE-TOOLS": "ruff,shellcheck", "INPUT_CONFIG": "repository: {runners: []}"}
	var out strings.Builder

	a := &action{
		args:    args("workflows/broken.yaml", "default", "", "", ".", "results/out.txt", "true"),
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

func TestActionGitHubAnnotationPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file, workingDir, want string
	}{
		{"repository discovery", "", "sub", ".github/workflows/broken.yaml"},
		{"relative file", "broken.yaml", "sub", "sub/broken.yaml"},
		{"parent file", "../.github/workflows/broken.yaml", "sub", ".github/workflows/broken.yaml"},
		{"workspace root", "sub/broken.yaml", ".", "sub/broken.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workspace := nativeWorkspaceWith(t, map[string]string{
				".git": "", ".github/workflows/broken.yaml": brokenWorkflow, "sub/broken.yaml": brokenWorkflow,
			})
			outputPath := filepath.Join(t.TempDir(), "output")
			resultPath := filepath.Join(t.TempDir(), "result.json")
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath, "ACTIONLINT_ACTION_RESULT": resultPath, "INPUT_CONFIG": "repository: {runners: []}"}
			var out strings.Builder
			a := &action{
				args:   args(tc.file, "github", "", "", tc.workingDir, "annotations.txt", "true"),
				stdout: &out, env: func(name string) string { return env[name] },
				lint: runLinter, newID: fixedID("DELIM"), timeout: lintTimeout,
			}
			if code := a.run(); code != 1 {
				t.Fatalf("wanted findings but got %d: %s", code, out.String())
			}
			outputs := parseOutputs(read(t, outputPath))
			for name, content := range map[string]string{
				"stdout": out.String(), "output": outputs["output"], "output-file": read(t, filepath.Join(workspace, "annotations.txt")),
			} {
				if want := "::error file=" + tc.want + ","; !strings.Contains(content, want) {
					t.Errorf("%s: wanted %q in %q", name, want, content)
				}
			}
			var result persistedResult
			if err := json.Unmarshal([]byte(read(t, resultPath)), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("wanted one persisted diagnostic, got %d", len(result.Diagnostics))
			}
			want, err := filepath.Rel(filepath.Join(workspace, tc.workingDir), filepath.Join(workspace, tc.want))
			if err != nil {
				t.Fatal(err)
			}
			if got := filepath.ToSlash(result.Diagnostics[0].Path); got != filepath.ToSlash(want) {
				t.Errorf("persisted diagnostic must retain analysis-relative path: got %q, want %q", got, filepath.ToSlash(want))
			}
		})
	}
}

func TestActionIgnoreAndAutomaticConfigEndToEnd(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		inputYAML  string
		configName string
		wantCode   int
	}{
		{"no ignore", "ignore: ''\n", "", 1},
		{"quoted block pattern", `ignore: |
  'label "ubuntu-24.04-custom" is unknown.'
`, "", 1},
		{"block pattern", `ignore: |
  label "ubuntu-24.04-custom" is unknown\.
`, "", 0},
		{"quoted scalar pattern", "ignore: 'label \"ubuntu-24.04-custom\" is unknown\\.'\n", "", 0},
		{"automatic yaml config", "ignore: ''\n", "actionlint.yaml", 0},
		{"automatic yml config", "ignore: ''\n", "actionlint.yml", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
				files[".github/"+tc.configName] = "repository: {runners: [ubuntu-24.04-custom]}\n"
			}
			workspace := nativeWorkspaceWith(t, files)
			outputPath := filepath.Join(t.TempDir(), "output")
			env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath, "INPUT_DISABLE-TOOLS": "ruff,shellcheck"}
			if tc.configName == "" {
				env["INPUT_CONFIG"] = "repository: {runners: []}"
			}
			var out strings.Builder
			a := &action{
				args:    args("", "github", in.Ignore, "", ".", "", "true"),
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
	t.Parallel()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace := nativeWorkspaceWith(t, map[string]string{"sub/broken.yaml": brokenWorkflow})
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
	got := a.runLint(&lintRequest{workingDir: sub, files: []string{"broken.yaml"}, format: formatJSON, overlays: []repoconfig.Overlay{knownRunnerOverlay(t)}})

	if resolved(t, seen) != resolved(t, dir) {
		t.Errorf("wanted the process directory to remain %q while linting but got %q", dir, seen)
	}
	if got.code != commandstatus.ExitSuccessProblemFound {
		t.Fatalf("wanted the workflow in the requested directory linted but got %d: %s%s", got.code, got.stderr, got.stdout)
	}
	problems := got.report.Diagnostics
	if len(problems) != 1 || problems[0].Path != "broken.yaml" {
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
	t.Parallel()
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
			return knownFiles(&lintOutcome{"late output", "late log", commandstatus.ExitSuccessNoProblem}, 1)
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
		if got.code != commandstatus.ExitFailure || !strings.Contains(got.stderr, "actionlint timed out after") {
			t.Errorf("wanted an independent timeout result but got %#v", got)
		}
		if got.stdout != "" || (got.report.FileCount != nil) {
			t.Errorf("timeout result must not expose the unfinished analysis: %#v", got)
		}
	case <-time.After(lintCancellationGrace + time.Second):
		t.Error("timeout waited for the noncooperative linter to return")
	}
}

func TestActionRunLintReportsUnusableWorkingDirectory(t *testing.T) {
	t.Parallel()
	a := &action{timeout: time.Minute, lint: func(*lintRequest) *lintResult {
		t.Error("actionlint must not run when the working directory is unusable")
		return nil
	}}
	workspace := nativeWorkspaceWith(t, map[string]string{"file": "not a directory"})
	for _, path := range []string{"missing", "file"} {
		got := a.runLint(&lintRequest{workingDir: filepath.Join(workspace, path)})
		if got.code != commandstatus.ExitFailure || got.stderr == "" {
			t.Errorf("wanted a failure for %q but got %#v", path, got)
		}
	}
}
