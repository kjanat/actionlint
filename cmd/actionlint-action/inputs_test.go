package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func args(values ...string) []string {
	return append([]string{"actionlint-action"}, values...)
}

func defaultArgs() []string {
	return args("", "github", "", "", "true", "true", ".", "", "true")
}

func isInputError(t *testing.T, err error) *inputError {
	t.Helper()
	if err == nil {
		t.Fatal("wanted an input error but got none")
	}
	var e *inputError
	if !errors.As(err, &e) {
		t.Fatalf("wanted an input error but got %#v", err)
	}
	return e
}

func TestParseInputsDefaults(t *testing.T) {
	in, err := parseInputs(defaultArgs())
	if err != nil {
		t.Fatal(err)
	}
	if len(in.files) != 0 || len(in.ignore) != 0 {
		t.Errorf("wanted no files and no ignore patterns but got %#v and %#v", in.files, in.ignore)
	}
	if in.format != formatGitHub {
		t.Errorf("wanted the github format but got %q", in.format)
	}
	if !in.shellcheck || !in.pyflakes || !in.failOnError {
		t.Errorf("wanted every boolean input to be true but got %#v", in)
	}
	if in.workingDirectory != "." || in.configFile != "" || in.outputFile != "" {
		t.Errorf("wanted the default paths but got %#v", in)
	}
}

func TestParseInputsArgumentCount(t *testing.T) {
	for _, tc := range [][]string{
		args(),
		args("", "github", "", "", "true", "true", ".", ""),
		args("", "github", "", "", "true", "true", ".", "", "true", "extra"),
	} {
		if _, err := parseInputs(tc); err == nil {
			t.Errorf("wanted an error for %d arguments", len(tc))
		} else if want := "The action received an unexpected number of inputs"; isInputError(t, err).Error() != want {
			t.Errorf("wanted %q but got %q", want, err)
		}
	}
}

func TestParseInputsFormats(t *testing.T) {
	for _, want := range formats {
		a := defaultArgs()
		a[2] = string(want)
		in, err := parseInputs(a)
		if err != nil {
			t.Fatalf("format %q: %v", want, err)
		}
		if in.format != want {
			t.Errorf("wanted format %q but got %q", want, in.format)
		}
	}

	for _, bad := range []string{"", "GitHub", "sarif ", "text"} {
		a := defaultArgs()
		a[2] = bad
		_, err := parseInputs(a)
		want := "Input 'format' must be github, default, oneline, json, json-lines, markdown, or sarif"
		if got := isInputError(t, err).Error(); got != want {
			t.Errorf("format %q: wanted %q but got %q", bad, want, got)
		}
	}
}

func TestParseInputsBooleans(t *testing.T) {
	for _, tc := range []struct {
		index int
		name  string
	}{
		{5, "shellcheck"},
		{6, "pyflakes"},
		{9, "fail-on-error"},
	} {
		for _, bad := range []string{"", "True", "FALSE", "1", "yes", " true"} {
			a := defaultArgs()
			a[tc.index] = bad
			_, err := parseInputs(a)
			want := "Input '" + tc.name + "' must be 'true' or 'false'"
			if got := isInputError(t, err).Error(); got != want {
				t.Errorf("%s=%q: wanted %q but got %q", tc.name, bad, want, got)
			}
		}
		for _, good := range []struct {
			value string
			want  bool
		}{{"true", true}, {"false", false}} {
			a := defaultArgs()
			a[tc.index] = good.value
			in, err := parseInputs(a)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{"shellcheck": in.shellcheck, "pyflakes": in.pyflakes, "fail-on-error": in.failOnError}[tc.name]
			if got != good.want {
				t.Errorf("%s=%q: wanted %v but got %v", tc.name, good.value, good.want, got)
			}
		}
	}
}

func TestSplitLines(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  []string
	}{
		{"", []string{}},
		{"\n\n", []string{}},
		{"a", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"\na\n\nb\n", []string{"a", "b"}},
		{" \na", []string{" ", "a"}},
	} {
		got := splitLines(tc.input)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("splitLines(%q) = %#v, wanted %#v", tc.input, got, tc.want)
		}
	}
}

func TestParseInputsRejectsOptionLikeFiles(t *testing.T) {
	for _, files := range []string{"--help", "workflow.yaml\n-color", "-"} {
		a := defaultArgs()
		a[1] = files
		_, err := parseInputs(a)
		want := "Input 'files' entries must not start with '-'; provide workflow file paths"
		if got := isInputError(t, err).Error(); got != want {
			t.Errorf("files=%q: wanted %q but got %q", files, want, got)
		}
	}
}

func TestWithinWorkspace(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(workspace, "sub", "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		value string
		want  string
	}{
		{"", workspace},
		{".", workspace},
		{"sub", filepath.Join(workspace, "sub")},
		{"sub/dir", filepath.Join(workspace, "sub", "dir")},
		{"./sub/../sub", filepath.Join(workspace, "sub")},
		{filepath.Join(workspace, "sub"), filepath.Join(workspace, "sub")},
	} {
		got, err := withinWorkspace(workspace, tc.value, "working-directory", true)
		if err != nil {
			t.Errorf("%q: %v", tc.value, err)
		} else if got != tc.want {
			t.Errorf("%q: wanted %q but got %q", tc.value, tc.want, got)
		}
	}

	for _, value := range []string{"..", "../elsewhere", "sub/../../elsewhere", filepath.Join(filepath.Dir(workspace), "elsewhere")} {
		_, err := withinWorkspace(workspace, value, "working-directory", false)
		want := "Input 'working-directory' must stay within the repository workspace"
		if got := isInputError(t, err).Error(); got != want {
			t.Errorf("%q: wanted %q but got %q", value, want, got)
		}
	}

	for _, value := range []string{"missing", "file.txt"} {
		_, err := withinWorkspace(workspace, value, "working-directory", true)
		want := "Input 'working-directory' must identify an existing directory"
		if got := isInputError(t, err).Error(); got != want {
			t.Errorf("%q: wanted %q but got %q", value, want, got)
		}
	}

	if got, err := withinWorkspace(workspace, "missing/deep.json", "output-file", false); err != nil {
		t.Error(err)
	} else if want := filepath.Join(workspace, "missing", "deep.json"); got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestWithinWorkspaceFollowsSymlinks(t *testing.T) {
	workspace := resolved(t, t.TempDir())
	outside := resolved(t, t.TempDir())
	if err := os.Symlink(outside, filepath.Join(workspace, "escape")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	_, err := withinWorkspace(workspace, "escape", "working-directory", true)
	want := "Input 'working-directory' must stay within the repository workspace"
	if got := isInputError(t, err).Error(); got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestResolveOutputFile(t *testing.T) {
	workspace := resolved(t, t.TempDir())

	if got, err := resolveOutputFile(workspace, ""); err != nil || got != "" {
		t.Errorf("wanted no output file but got %q and %v", got, err)
	}

	if got, err := resolveOutputFile(workspace, "out/results.json"); err != nil {
		t.Error(err)
	} else if want := filepath.Join(workspace, "out", "results.json"); got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}

	if err := os.Mkdir(filepath.Join(workspace, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{".", "sub"} {
		_, err := resolveOutputFile(workspace, value)
		want := "Input 'output-file' must not identify a directory"
		if got := isInputError(t, err).Error(); got != want {
			t.Errorf("%q: wanted %q but got %q", value, want, got)
		}
	}

	_, err := resolveOutputFile(workspace, "../escaped.json")
	want := "Input 'output-file' must stay within the repository workspace"
	if got := isInputError(t, err).Error(); got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestResolvePathRelativeToProcessDirectory(t *testing.T) {
	dir := resolved(t, t.TempDir())
	t.Chdir(dir)

	got, err := resolvePath("nested/leaf")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "nested", "leaf"); got != want {
		t.Errorf("wanted %q but got %q", want, got)
	}
}

func TestContainsRejectsSiblingPrefix(t *testing.T) {
	workspace := filepath.Join(string(filepath.Separator), "github", "workspace")
	if contains(workspace, workspace+"-other") {
		t.Error("a sibling directory sharing the workspace prefix must not be contained")
	}
	if !contains(workspace, workspace) {
		t.Error("the workspace itself must be contained")
	}
	if !contains(workspace, filepath.Join(workspace, "sub")) {
		t.Error("a subdirectory must be contained")
	}
}

func resolved(t *testing.T, dir string) string {
	t.Helper()
	got, err := resolvePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Clean(got)
	}
	return got
}
