package cli

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestRootFlagsBetweenFilenames(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name       string
		args, want []string
		terminated bool
		json       bool
	}{
		{name: "after file", args: []string{"first.yml", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "between files", args: []string{"first.yml", "--json", "second.yml"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "after stdin", args: []string{"-", "--json"}, want: []string{"-"}, json: true},
		{name: "single dash option", args: []string{"first.yml", "-json"}, want: []string{"first.yml"}, json: true},
		{name: "tool flag", args: []string{"first.yml", "--tool", "ruff", "second.yml", "--json"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "linter command", args: []string{"first.yml", "--tool=ruff=uvx ruff", "second.yml", "--json"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "option-like value", args: []string{"first.yml", "--stdin-filename", "--json", "-"}, want: []string{"first.yml", "-"}},
		{name: "legacy empty linter", args: []string{"first.yml", "-shellcheck=", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "delimiter value", args: []string{"first.yml", "--stdin-filename", "--", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "delimiter", args: []string{"first.yml", "--", "--json", "-h"}, want: []string{"first.yml", "--json", "-h"}, terminated: true},
		{name: "delimiter after flag", args: []string{"first.yml", "--json", "--", "--help"}, want: []string{"first.yml", "--help"}, terminated: true, json: true},
		{name: "command after file", args: []string{"first.yml", "version", "--json"}, want: []string{"first.yml", "version"}, json: true},
		{name: "command boundary", args: []string{"check", "first.yml", "--json"}, want: []string{"check", "first.yml", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := newCommandApp(t.Context(), &Command{})
			rest, terminated, err := app.parseRoot(tc.args)
			if err != nil || !slices.Equal(rest, tc.want) || terminated != tc.terminated || app.inv.JSON != tc.json || app.errorJSON != tc.json {
				t.Fatalf("paths=%v terminated=%v json=%v errorJSON=%v err=%v", rest, terminated, app.inv.JSON, app.errorJSON, err)
			}
		})
	}
}

func TestRootUsesCommandFlagDefinitions(t *testing.T) {
	t.Parallel()
	app := newCommandApp(t.Context(), &Command{})
	var enabled bool
	app.root.Flags().BoolVarP(&enabled, "test-option", "t", false, "")
	rest, _, err := app.parseRoot([]string{"-t", "input.yml"})
	if err != nil || !enabled || !app.set["test-option"] || len(rest) != 1 || rest[0] != "input.yml" {
		t.Fatalf("root did not use the shared flag definition: enabled=%v rest=%v err=%v", enabled, rest, err)
	}
}

func TestCheckVersionFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, command := range []string{"check", "fix", "pre-commit"} {
		for _, flag := range []string{"--version", "-V", "-version"} {
			t.Run(command+"/"+flag, func(t *testing.T) {
				t.Parallel()
				var stdout, stderr bytes.Buffer
				cmd := Command{Stdout: &stdout, Stderr: &stderr}
				status := cmd.Main([]string{"actionlint", command, flag})
				if status != 0 || !strings.Contains(stdout.String(), "Build: ") || stderr.Len() != 0 {
					t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
				}
			})
		}
	}
}

func TestSubcommandLegacyFlagBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		args, want []string
	}{
		{"after file", []string{"check", "input.yml", "-shellcheck=tool -x"}, []string{"check", "input.yml", "--shellcheck=tool -x"}},
		{"option-like value", []string{"check", "-stdin-filename", "-shellcheck", "-"}, []string{"check", "--stdin-filename", "-shellcheck", "-"}},
		{"delimiter value", []string{"check", "-stdin-filename", "--", "-json"}, []string{"check", "--stdin-filename", "--", "--json"}},
		{"literal filename", []string{"check", "--", "-shellcheck"}, []string{"check", "--", "-shellcheck"}},
		{"nested command", []string{"config", "show", "-origin"}, []string{"config", "show", "--origin"}},
		{"parent flag", []string{"config", "-no-config", "show", "-origin"}, []string{"config", "--no-config", "show", "--origin"}},
		{"command-like value", []string{"config", "-config", "show", "show", "-origin"}, []string{"config", "--config", "show", "show", "--origin"}},
		{"short value", []string{"check", "-f", "-shellcheck", "-"}, []string{"check", "--template", "-shellcheck", "-"}},
		{"short flags", []string{"check", "-q", "-ojson", "-"}, []string{"check", "--quiet", "--output-format=json", "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := newCommandApp(t.Context(), &Command{})
			if got := normalizeCommandArgs(app.root, tc.args); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommandNameSelectsTheCommandBesideAFileWithThatName(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("version", []byte("on: push\njobs: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		cmd := Command{Stdout: &stdout, Stderr: &stderr}
		status := cmd.Main(append([]string{"actionlint", "--no-config", "--summary=false", "--no-tool=shellcheck", "--no-tool=ruff"}, args...))
		return status, stdout.String(), stderr.String()
	}
	if status, stdout, stderr := run("version"); status != 0 || !strings.Contains(stdout, "Build: ") || stderr != "" {
		t.Fatalf("version: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	for _, args := range [][]string{{"./version"}, {"--", "version"}, {"check", "version"}} {
		if status, stdout, stderr := run(args...); status != 1 || !strings.Contains(stdout+stderr, "version") {
			t.Fatalf("%v: status=%d stdout=%q stderr=%q", args, status, stdout, stderr)
		}
	}
}
