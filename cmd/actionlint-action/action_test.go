package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev"
)

type recordedLint struct {
	req     *lintRequest
	outcome *lintOutcome
}

func (r *recordedLint) run(req *lintRequest) *lintOutcome {
	r.req = req
	return r.outcome
}

type actionRun struct {
	code    int
	stdout  string
	outputs map[string]string
	action  *action
}

func runAction(t *testing.T, workspace string, outcome *lintOutcome, argv ...string) (*actionRun, *recordedLint) {
	t.Helper()
	recorder := &recordedLint{outcome: outcome}
	outputPath := filepath.Join(t.TempDir(), "output")
	var out strings.Builder
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath}

	a := &action{
		args:    args(argv...),
		stdout:  &out,
		env:     func(name string) string { return env[name] },
		lint:    recorder.run,
		newID:   fixedID("DELIM"),
		timeout: time.Minute,
	}
	code := a.run()

	outputs := map[string]string{}
	if b, err := os.ReadFile(outputPath); err == nil {
		outputs = parseOutputs(string(b))
	}
	return &actionRun{code, out.String(), outputs, a}, recorder
}

func parseOutputs(content string) map[string]string {
	values := map[string]string{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		name, delimiter, ok := strings.Cut(lines[i], "<<")
		if !ok {
			continue
		}
		body := []string{}
		for i++; i < len(lines) && lines[i] != delimiter; i++ {
			body = append(body, lines[i])
		}
		values[name] = strings.Join(body, "\n")
	}
	return values
}

func problemJSON() string {
	return `[{"message":"m","filepath":"w.yaml","line":1,"column":2,"kind":"k","snippet":"a\n^","end_column":3}]` + "\n"
}

func TestActionReportsSuccess(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	run, recorder := runAction(t, workspace, &lintOutcome{"[]\n", "", actionlint.ExitStatusSuccessNoProblem},
		"", "json", "", "", "true", "true", ".", "", "true")

	if run.code != 0 {
		t.Errorf("wanted exit code 0 but got %d: %s", run.code, run.stdout)
	}
	want := map[string]string{
		"exit-code":      "0",
		"result":         "success",
		"problems-found": "false",
		"problem-count":  "0",
		"output":         "[]",
		"output-file":    "",
	}
	for name, value := range want {
		if run.outputs[name] != value {
			t.Errorf("output %q: wanted %q but got %q", name, value, run.outputs[name])
		}
	}
	if want := "::stop-commands::DELIM\n[]\n::DELIM::\n"; run.stdout != want {
		t.Errorf("wanted %q but got %q", want, run.stdout)
	}
	if recorder.req.workingDir != workspace {
		t.Errorf("wanted the workspace as the working directory but got %q", recorder.req.workingDir)
	}
	if recorder.req.shellcheck != "shellcheck" || recorder.req.pyflakes != "pyflakes" {
		t.Errorf("wanted both external commands enabled but got %#v", recorder.req)
	}
	if recorder.req.format != "{{json .}}" {
		t.Errorf("wanted the JSON format template but got %q", recorder.req.format)
	}
}

func TestActionReportsProblems(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	run, _ := runAction(t, workspace, &lintOutcome{problemJSON(), "", actionlint.ExitStatusSuccessProblemFound},
		"", "github", "", "", "true", "true", ".", "", "true")

	if run.code != 1 {
		t.Errorf("wanted exit code 1 but got %d", run.code)
	}
	if run.outputs["result"] != "problems-found" || run.outputs["problems-found"] != "true" || run.outputs["problem-count"] != "1" {
		t.Errorf("wanted a single problem but got %#v", run.outputs)
	}
	want := "::error file=w.yaml,line=1,col=2,endColumn=3,title=actionlint (k)::m%0A%0Aa%0A^\n"
	if run.stdout != want {
		t.Errorf("wanted %q but got %q", want, run.stdout)
	}
}

func TestActionKeepsProblemsNonFatal(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	run, _ := runAction(t, workspace, &lintOutcome{problemJSON(), "", actionlint.ExitStatusSuccessProblemFound},
		"", "oneline", "", "", "true", "true", ".", "", "false")

	if run.code != 0 {
		t.Errorf("wanted exit code 0 when fail-on-error is false but got %d", run.code)
	}
	if run.outputs["exit-code"] != "1" || run.outputs["problems-found"] != "true" {
		t.Errorf("wanted the actionlint exit code preserved in the outputs but got %#v", run.outputs)
	}
}

func TestActionReportsFailure(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	run, _ := runAction(t, workspace, &lintOutcome{"", "could not read \"w.yaml\"\n", actionlint.ExitStatusFailure},
		"", "json", "", "", "true", "true", ".", "", "false")

	if run.code != 3 {
		t.Errorf("wanted exit code 3 but got %d", run.code)
	}
	if run.outputs["result"] != "failure" || run.outputs["problem-count"] != "" {
		t.Errorf("wanted a failure without a problem count but got %#v", run.outputs)
	}
	if want := "::error title=actionlint failed::could not read \"w.yaml\"%0A\n"; run.stdout != want {
		t.Errorf("wanted %q but got %q", want, run.stdout)
	}
}

func TestActionReportsInvalidInput(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	run, recorder := runAction(t, workspace, nil, "", "text", "", "", "true", "true", ".", "", "true")

	if run.code != 2 {
		t.Errorf("wanted exit code 2 but got %d", run.code)
	}
	if recorder.req != nil {
		t.Error("wanted actionlint not to run for an invalid input")
	}
	want := "::error title=Invalid action input::Input 'format' must be github, default, oneline, json, json-lines, markdown, or sarif\n"
	if run.stdout != want {
		t.Errorf("wanted %q but got %q", want, run.stdout)
	}
	if len(run.outputs) != 0 {
		t.Errorf("wanted no outputs but got %#v", run.outputs)
	}
}

func TestActionWritesOutputFile(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	run, _ := runAction(t, workspace, &lintOutcome{problemJSON(), "", actionlint.ExitStatusSuccessProblemFound},
		"", "json-lines", "", "", "true", "true", ".", "results/out.jsonl", "false")

	if run.code != 0 {
		t.Errorf("wanted exit code 0 but got %d", run.code)
	}
	if run.outputs["output-file"] != "results/out.jsonl" {
		t.Errorf("wanted the repository relative path but got %q", run.outputs["output-file"])
	}
	content := read(t, filepath.Join(workspace, "results", "out.jsonl"))
	if !strings.HasPrefix(content, `{"message":"m"`) || !strings.HasSuffix(content, "\n") {
		t.Errorf("wanted one JSON object per line but got %q", content)
	}
}

func TestActionRejectsEscapingPaths(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"working-directory", []string{"", "json", "", "", "true", "true", "..", "", "true"}},
		{"output-file", []string{"", "json", "", "", "true", "true", ".", "../escaped.json", "true"}},
		{"config-file", []string{"", "json", "", "../actionlint.yaml", "true", "true", ".", "", "true"}},
		{"files", []string{"../outside.yaml", "json", "", "", "true", "true", ".", "", "true"}},
	} {
		run, recorder := runAction(t, workspace, nil, tc.argv...)
		if run.code != 2 {
			t.Errorf("%s: wanted exit code 2 but got %d", tc.name, run.code)
		}
		if recorder.req != nil {
			t.Errorf("%s: wanted actionlint not to run", tc.name)
		}
		want := "::error title=Invalid action input::Input '" + tc.name + "' must stay within the repository workspace\n"
		if run.stdout != want {
			t.Errorf("%s: wanted %q but got %q", tc.name, want, run.stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(workspace), "escaped.json")); err == nil {
		t.Error("the escaping output file must not be written")
	}
}

func TestActionReportsMissingWorkspace(t *testing.T) {
	workspace := filepath.Join(resolved(t, t.TempDir()), "missing")
	run, recorder := runAction(t, workspace, nil, "", "json", "", "", "true", "true", ".", "", "true")

	if run.code != 3 {
		t.Errorf("wanted exit code 3 but got %d", run.code)
	}
	if recorder.req != nil {
		t.Error("wanted actionlint not to run without a workspace")
	}
	if !strings.Contains(run.stdout, "::error title=actionlint action failed::") {
		t.Errorf("wanted a failure annotation but got %q", run.stdout)
	}
}

func TestActionPassesInputsToLinter(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(workspace, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "sub", "w.yaml"), []byte("on: push\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "sub", "conf.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, recorder := runAction(t, workspace, &lintOutcome{"[]\n", "", actionlint.ExitStatusSuccessNoProblem},
		"w.yaml\n\nw.yaml", "sarif", "first\nsecond", "conf.yaml", "false", "false", "sub", "", "true")

	req := recorder.req
	if req == nil {
		t.Fatal("wanted actionlint to run")
	}
	if want := filepath.Join(workspace, "sub"); req.workingDir != want {
		t.Errorf("wanted the working directory %q but got %q", want, req.workingDir)
	}
	if want := filepath.Join(workspace, "sub", "conf.yaml"); req.configFile != want {
		t.Errorf("wanted the config file %q but got %q", want, req.configFile)
	}
	if strings.Join(req.files, "|") != "w.yaml|w.yaml" {
		t.Errorf("wanted the given file paths but got %#v", req.files)
	}
	if strings.Join(req.ignore, "|") != "first|second" {
		t.Errorf("wanted both ignore patterns but got %#v", req.ignore)
	}
	if req.shellcheck != "" || req.pyflakes != "" {
		t.Errorf("wanted both external commands disabled but got %#v", req)
	}
	if req.format != sarifTemplate {
		t.Errorf("wanted the SARIF template but got %q", req.format)
	}
}

func TestActionFallsBackToProcessDirectory(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	t.Chdir(workspace)

	var out strings.Builder
	recorder := &recordedLint{outcome: &lintOutcome{"[]\n", "", actionlint.ExitStatusSuccessNoProblem}}
	a := &action{
		args:    args("", "json", "", "", "true", "true", ".", "", "true"),
		stdout:  &out,
		env:     func(string) string { return "" },
		lint:    recorder.run,
		newID:   fixedID("DELIM"),
		timeout: time.Minute,
	}
	if code := a.run(); code != 0 {
		t.Errorf("wanted exit code 0 but got %d: %s", code, out.String())
	}
	if recorder.req.workingDir != workspace {
		t.Errorf("wanted the process directory as the workspace but got %q", recorder.req.workingDir)
	}
}

func TestActionTimesOut(t *testing.T) {
	if lintTimeout != 300*time.Second {
		t.Errorf("wanted a 300 second lint timeout but got %s", lintTimeout)
	}

	workspace := resolved(t, t.TempDir())
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	var out strings.Builder
	outputPath := filepath.Join(t.TempDir(), "output")
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "GITHUB_OUTPUT": outputPath}
	a := &action{
		args:   args("", "json", "", "", "true", "true", ".", "", "true"),
		stdout: &out,
		env:    func(name string) string { return env[name] },
		lint: func(*lintRequest) *lintOutcome {
			<-release
			return &lintOutcome{"[]\n", "", actionlint.ExitStatusSuccessNoProblem}
		},
		newID:   fixedID("DELIM"),
		timeout: 10 * time.Millisecond,
	}

	if code := a.run(); code != 3 {
		t.Errorf("wanted exit code 3 but got %d", code)
	}
	if !strings.Contains(out.String(), "actionlint timed out after") {
		t.Errorf("wanted a timeout annotation but got %q", out.String())
	}
	outputs := parseOutputs(read(t, outputPath))
	if outputs["result"] != "failure" || outputs["exit-code"] != "3" {
		t.Errorf("wanted a failure result but got %#v", outputs)
	}
}
