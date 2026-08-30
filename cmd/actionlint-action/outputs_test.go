package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixedID(values ...string) func() string {
	i := 0
	return func() string {
		v := values[min(i, len(values)-1)]
		i++
		return v
	}
}

func newTestAction(t *testing.T, out *strings.Builder, env map[string]string) *action {
	t.Helper()
	return &action{
		stdout: out,
		env:    func(name string) string { return env[name] },
		newID:  fixedID("DELIM"),
	}
}

func TestAppendOutputsWithoutEnvironmentFile(t *testing.T) {
	a := newTestAction(t, &strings.Builder{}, map[string]string{})
	if err := a.appendOutputs([]namedOutput{{"result", "success"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAppendOutputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	a := newTestAction(t, &strings.Builder{}, map[string]string{"GITHUB_OUTPUT": path})

	err := a.appendOutputs([]namedOutput{
		{"exit-code", "1"},
		{"problem-count", ""},
		{"output", "first\nsecond"},
		{"trailing", "line\n"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	want := "exit-code<<DELIM\n1\nDELIM\n" +
		"problem-count<<DELIM\nDELIM\n" +
		"output<<DELIM\nfirst\nsecond\nDELIM\n" +
		"trailing<<DELIM\nline\nDELIM\n"
	if got != want {
		t.Errorf("wanted\n%q\nbut got\n%q", want, got)
	}
}

func TestAppendOutputsAppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(path, []byte("existing<<X\nvalue\nX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestAction(t, &strings.Builder{}, map[string]string{"GITHUB_OUTPUT": path})
	if err := a.appendOutputs([]namedOutput{{"result", "success"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, path), "existing<<X\nvalue\nX\nresult<<DELIM\nsuccess\nDELIM\n"; got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestAppendOutputsAvoidsDelimiterCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	a := newTestAction(t, &strings.Builder{}, map[string]string{"GITHUB_OUTPUT": path})
	a.newID = fixedID("DELIM")

	if err := a.appendOutputs([]namedOutput{{"output", "before\nDELIM\nDELIM_\nafter"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, path), "output<<DELIM__\nbefore\nDELIM\nDELIM_\nafter\nDELIM__\n"; got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestWriteResultFileWithoutTarget(t *testing.T) {
	got, err := writeResultFile(openRoot(t, t.TempDir()), "", "content")
	if err != nil || got != "" {
		t.Errorf("wanted no written file but got %q and %v", got, err)
	}
}

func TestWriteResultFileCreatesMissingDirectories(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	target := filepath.Join("a", "b", "results.json")

	got, err := writeResultFile(openRoot(t, workspace), target, "content")
	if err != nil {
		t.Fatal(err)
	}
	if want := "a/b/results.json"; got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
	if content := read(t, filepath.Join(workspace, target)); content != "content" {
		t.Errorf("wanted the rendered content but got %q", content)
	}
}

func TestWriteResultFileOverwritesExistingFile(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(workspace, "results.json"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := writeResultFile(openRoot(t, workspace), "results.json", "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if got != "results.json" {
		t.Errorf("wanted %q but got %q", "results.json", got)
	}
	if content := read(t, filepath.Join(workspace, "results.json")); content != "fresh" {
		t.Errorf("wanted the file overwritten but got %q", content)
	}
}

func TestEmitSkipsEmptyOutput(t *testing.T) {
	var out strings.Builder
	a := newTestAction(t, &out, map[string]string{})
	a.emit("", 0, formatDefault)
	if out.String() != "" {
		t.Errorf("wanted no output but got %q", out.String())
	}
}

func TestEmitStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		count   string
		files   int
		in      *inputs
		contain string
	}{
		{"clean", 0, "0", 2, &inputs{shellcheck: true, pyflakes: true}, ": 0 problems in 2 workflow files (shellcheck, pyflakes)\n"},
		{"problem", 1, "1", 1, &inputs{shellcheck: true}, ": 1 problem in 1 workflow file (shellcheck)\n"},
		{"zero files", 0, "0", 0, &inputs{}, ": 0 problems in 0 workflow files (external linters disabled)\n"},
		{"failure", 3, "", 0, &inputs{pyflakes: true}, ": failed while checking 0 workflow files (pyflakes)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			a := newTestAction(t, &out, map[string]string{})
			a.emitStatus(tc.code, tc.count, tc.files, tc.in)
			if !strings.HasPrefix(out.String(), "actionlint ") || !strings.HasSuffix(out.String(), tc.contain) {
				t.Errorf("wanted a versioned status ending in %q but got %q", tc.contain, out.String())
			}
		})
	}
}

func TestEmitAnnotatesFailures(t *testing.T) {
	for _, code := range []int{2, 3} {
		var out strings.Builder
		a := newTestAction(t, &out, map[string]string{})
		a.emit("boom\ndetails", code, formatDefault)
		if want := "::error title=actionlint failed::boom%0Adetails\n"; out.String() != want {
			t.Errorf("code %d: wanted %q but got %q", code, want, out.String())
		}
	}
}

func TestEmitWritesAnnotationsVerbatim(t *testing.T) {
	var out strings.Builder
	a := newTestAction(t, &out, map[string]string{})
	rendered := "::error file=w.yaml,line=1,col=1,endColumn=1,title=actionlint (k)::m\n"
	a.emit(rendered, 1, formatGitHub)
	if out.String() != rendered {
		t.Errorf("wanted %q but got %q", rendered, out.String())
	}
}

func TestEmitStopsWorkflowCommands(t *testing.T) {
	var out strings.Builder
	a := newTestAction(t, &out, map[string]string{})
	a.emit("line\n", 1, formatDefault)
	if want := "::stop-commands::DELIM\nline\n::DELIM::\n"; out.String() != want {
		t.Errorf("wanted %q but got %q", want, out.String())
	}
}

func TestEmitTerminatesOutputWithoutTrailingNewline(t *testing.T) {
	var out strings.Builder
	a := newTestAction(t, &out, map[string]string{})
	a.emit("line", 1, formatJSON)
	if want := "::stop-commands::DELIM\nline\n::DELIM::\n"; out.String() != want {
		t.Errorf("wanted %q but got %q", want, out.String())
	}
}

func TestNewDelimiterIsUnique(t *testing.T) {
	first, second := newDelimiter(), newDelimiter()
	if first == second {
		t.Errorf("wanted unique delimiters but got %q twice", first)
	}
	if !strings.HasPrefix(first, "actionlint_") {
		t.Errorf("wanted an actionlint prefix but got %q", first)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
