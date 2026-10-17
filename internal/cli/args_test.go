package cli

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestModernRepeatedIgnoreAliases(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--ignore-regex", "undefined variable", "--ignore", "other", "-"},
		{"check", "--ignore", "undefined variable", "--ignore-regex", "other", "-"},
		{"--ignore", "undefined variable", "check", "--ignore-regex", "other", "-"},
	} {
		if got := testRunCommand(commandBadWorkflow, args...); got != (commandTranscript{}) {
			t.Fatalf("repeated patterns were lost: %q: %+v", args, got)
		}
	}
}

func TestCommandArgumentCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		status   int
		contains string
	}{
		{"long", []string{"--oneline", "-"}, 1, "[expression-reference]"},
		{"legacy", []string{"-oneline", "-"}, 1, "[expression-reference]"},
		{"short format", []string{"-f", "{{range .}}{{.Kind}}{{end}}", "-"}, 1, "expression-reference"},
		{"short output", []string{"-o", "json", "-"}, 1, `"rule":"expression-reference"`},
		{"option-like value", []string{"--stdin-filename", "-completions", "-"}, 1, "-completions:6:"},
		{"option-like equal value", []string{"-stdin-filename=-completions", "-"}, 1, "-completions:6:"},
		{"ignore literal flag", []string{"-ignore", "--json", "-"}, 1, "[expression-reference]"},
		{"true value", []string{"--oneline=TRUE", "-"}, 1, "[expression-reference]"},
		{"unknown", []string{"--unknown"}, 2, "unknown flag"},
		{"missing value", []string{"--output"}, 2, "needs an argument"},
		{"invalid bool", []string{"--oneline=maybe"}, 2, "invalid argument"},
		{"unknown shell", []string{"--completion", "not-a-shell"}, 2, "shell"},
		{"help exits early", []string{"--help", "--unknown"}, 0, "Usage:"},
		{"legacy help value", []string{"-help=false"}, 0, "Usage:"},
		{"help short", []string{"-h"}, 0, "Usage:"},
		{"invalid before help", []string{"--unknown", "--help"}, 2, "unknown flag"},
		{"format conflict", []string{"--json", "--format={{json .}}", "-"}, 2, "cannot be combined"},
		{"output conflict", []string{"--json", "--output=jsonl", "-"}, 2, "cannot be combined"},
		{"bad output", []string{"--output=csv", "-"}, 2, "invalid output mode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := testRunCommand(commandBadWorkflow, tc.args...)
			if got.Status != tc.status || !strings.Contains(got.Stdout+got.Stderr, tc.contains) {
				t.Fatalf("got %+v, want status=%d containing %q", got, tc.status, tc.contains)
			}
		})
	}
}

func TestRootAndCheckArgumentParity(t *testing.T) {
	for _, args := range [][]string{
		{"-qv", "-"},
		{"-qojson", "-"},
		{"-qvo", "json", "-"},
		{"-q=false", "--oneline", "-"},
		{"-stdin-filename", "check", "--json", "-"},
		{"--stdin-filename", "--json", "-"},
		{"-ignore", "--json", "-"},
		{"--ignore", "undefined variable", "-ignore", "other", "-"},
		{"--unknown"},
		{"--output"},
		{"-oneline=maybe"},
		{"--unknown", "-qofalse"},
	} {
		root := testRunCommand(commandBadWorkflow, args...)
		check := testRunCommand(commandBadWorkflow, append([]string{"check"}, args...)...)
		if root != check {
			t.Errorf("%q: root=%+v check=%+v", args, root, check)
		}
	}
}

func TestCommandLiteralFilenames(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"first.yml", "--json", "-completions", "--help", "version", "completion", "__complete"} {
		if err := os.WriteFile(name, []byte(commandGoodWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"first.yml", "--", "--json", "-completions", "--help"},
		{"--", "--json", "--help", "-completions", "__complete"},
		{"version", "completion"},
	} {
		got := testRunCommand("", args...)
		if got != (commandTranscript{}) {
			t.Errorf("%q: %+v", args, got)
		}
	}
}

func TestRootFlagsBetweenFilenames(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name       string
		args, want []string
		forced     string
		terminated bool
		json       bool
	}{
		{name: "after file", args: []string{"first.yml", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "between files", args: []string{"first.yml", "--json", "second.yml"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "after stdin", args: []string{"-", "--json"}, want: []string{"-"}, json: true},
		{name: "single dash option", args: []string{"first.yml", "-json"}, want: []string{"first.yml"}, json: true},
		{name: "optional linter", args: []string{"first.yml", "--ruff", "second.yml", "--json"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "linter command", args: []string{"first.yml", "--ruff=uvx ruff", "second.yml", "--json"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "option-like value", args: []string{"first.yml", "--stdin-filename", "--json", "-"}, want: []string{"first.yml", "-"}},
		{name: "legacy bare linter", args: []string{"first.yml", "-ruff", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "delimiter value", args: []string{"first.yml", "--stdin-filename", "--", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "delimiter", args: []string{"first.yml", "--", "--json", "-h"}, want: []string{"first.yml", "--json", "-h"}, terminated: true},
		{name: "delimiter after flag", args: []string{"first.yml", "--json", "--", "--help"}, want: []string{"first.yml", "--help"}, terminated: true, json: true},
		{name: "command after file", args: []string{"first.yml", "version", "--json"}, want: []string{"first.yml", "version"}, json: true},
		{name: "command boundary", args: []string{"check", "first.yml", "--json"}, want: []string{"check", "first.yml", "--json"}},
		{name: "forced command", args: []string{"first.yml", "--command", "check", "--color=never", "--json"}, want: []string{"first.yml", "--color=never", "--json"}, forced: "check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newCommandApp(&Command{})
			rest, forced, terminated, err := app.parseRoot(tc.args)
			if err != nil || !slices.Equal(rest, tc.want) || forced != tc.forced || terminated != tc.terminated || app.inv.JSON != tc.json || app.errorJSON != tc.json {
				t.Fatalf("paths=%v forced=%q terminated=%v json=%v errorJSON=%v err=%v", rest, forced, terminated, app.inv.JSON, app.errorJSON, err)
			}
		})
	}
	for _, args := range [][]string{
		{"-", "--ignore-regex", "undefined variable", "--ignore", "other"},
		{"-", "-ignore", "other", "--ignore-regex", "undefined variable"},
	} {
		if got := testRunCommand(commandBadWorkflow, args...); got != (commandTranscript{}) {
			t.Fatalf("filters after stdin were lost: %v: %+v", args, got)
		}
	}
	for _, args := range [][]string{{"missing.yml", "--unknown", "--json"}, {"missing.yml", "--json", "--output-file"}} {
		got := testRunCommand("", args...)
		if got.Status != 2 || !strings.Contains(got.Stderr, `"exit_code":2`) {
			t.Fatalf("invalid options after filenames did not select JSON errors: %v: %+v", args, got)
		}
	}
}

func TestRootUsesCommandFlagDefinitions(t *testing.T) {
	app := newCommandApp(&Command{})
	var enabled bool
	app.root.Flags().BoolVarP(&enabled, "test-option", "t", false, "")
	rest, _, _, err := app.parseRoot([]string{"-t", "input.yml"})
	if err != nil || !enabled || !app.set["test-option"] || len(rest) != 1 || rest[0] != "input.yml" {
		t.Fatalf("root did not use the shared flag definition: enabled=%v rest=%v err=%v", enabled, rest, err)
	}
}

func TestSubcommandLegacyFlagBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args, want []string
	}{
		{"after file", []string{"check", "input.yml", "-shellcheck=tool -x"}, []string{"check", "input.yml", "--shellcheck=tool -x"}},
		{"option-like value", []string{"check", "-stdin-filename", "-shellcheck", "-"}, []string{"check", "--stdin-filename", "-shellcheck", "-"}},
		{"delimiter value", []string{"check", "-stdin-filename", "--", "-no-ruff"}, []string{"check", "--stdin-filename", "--", "--no-ruff"}},
		{"literal filename", []string{"check", "--", "-shellcheck"}, []string{"check", "--", "-shellcheck"}},
		{"nested command", []string{"config", "show", "-origin"}, []string{"config", "show", "--origin"}},
		{"parent flag", []string{"config", "-no-config", "show", "-origin"}, []string{"config", "--no-config", "show", "--origin"}},
		{"command-like value", []string{"config", "-config", "show", "show", "-origin"}, []string{"config", "--config", "show", "show", "--origin"}},
		{"short value", []string{"check", "-f", "-shellcheck", "-"}, []string{"check", "--template", "-shellcheck", "-"}},
		{"short flags", []string{"check", "-q", "-ojson", "-"}, []string{"check", "--quiet", "--output-format=json", "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newCommandApp(&Command{})
			if got := normalizeCommandArgs(app.root, tc.args); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
