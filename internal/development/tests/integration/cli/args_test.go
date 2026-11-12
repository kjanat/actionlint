package cli_test

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestModernRepeatedIgnoreFilters(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"check", "--ignore-regex", "undefined variable", "--ignore", "other", "-"},
		{"check", "--ignore-regex", "undefined variable", "--ignore-regex", "other", "-"},
		{"--ignore-regex", "undefined variable", "check", "--ignore-regex", "other", "-"},
	} {
		if got := testRunCommand(commandBadWorkflow, args...); got != (commandTranscript{}) {
			t.Fatalf("repeated patterns were lost: %q: %+v", args, got)
		}
	}
}

func TestCommandArgumentCompatibility(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := testRunCommand(commandBadWorkflow, tc.args...)
			if got.Status != tc.status || !strings.Contains(got.Stdout+got.Stderr, tc.contains) {
				t.Fatalf("got %+v, want status=%d containing %q", got, tc.status, tc.contains)
			}
		})
	}
}

func TestCommandRepeatedOutputs(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--output", "github", "--output", "pretty", "--no-config", "-"},
		{"check", "-o", "github", "--output", "pretty", "--no-config", "-"},
		{"--output", "github", "check", "--output-format", "pretty", "--no-config", "-"},
		{"-o", "github", "-o", "github", "-o", "pretty", "--no-config", "-"},
	} {
		got := testRunCommand(commandBadWorkflow, args...)
		annotation := strings.Index(got.Stdout, "::error ")
		pretty := strings.Index(got.Stdout, "error[expression-reference]")
		if got.Status != 1 || got.Stderr != "" || annotation != 0 || pretty <= annotation ||
			strings.Count(got.Stdout, "::error ") != 1 || strings.Count(got.Stdout, "error[expression-reference]") != 1 {
			t.Fatalf("outputs through %v: %+v", args, got)
		}
	}
	for _, command := range [][]string{nil, {"check"}} {
		got := testRunCommand(commandBadWorkflow, append(command, "--no-config", "-")...)
		if got.Status != 1 || !strings.HasPrefix(got.Stdout, "error[expression-reference]") {
			t.Fatalf("pretty default through %v: %+v", command, got)
		}
		got = testRunCommand(commandBadWorkflow, append(command, "--output", "pretty", "--output", "jsonl", "--no-config", "-")...)
		if got.Status != 1 || !strings.HasPrefix(got.Stdout, "error[expression-reference]") ||
			!strings.Contains(got.Stdout, `"rule":"expression-reference"`) {
			t.Fatalf("mixed JSON and pretty through %v: %+v", command, got)
		}
	}
}

func TestCommandTemplateGitHubEscaping(t *testing.T) {
	t.Parallel()
	format := "{{range .}}::error file={{.Filepath | escapeProperty}}::{{.Message | escapeData}}%0A{{.Snippet | escapeData}}{{end}}"
	input := strings.Replace(commandBadWorkflow, "echo", "echo 100%", 1)
	for _, command := range [][]string{nil, {"check"}} {
		args := slices.Concat(command, []string{"-format", format, "--stdin-filename", "workflow%,x.yml", "--no-config", "-"})
		got := testRunCommand(input, args...)
		if got.Status != 1 || got.Stderr != "" || !strings.HasPrefix(got.Stdout, "::error file=workflow%25%2Cx.yml::") ||
			!strings.Contains(got.Stdout, "100%25") || !strings.Contains(got.Stdout, "%0A") || strings.ContainsAny(got.Stdout, "\r\n") {
			t.Fatalf("template escaping through %v: %+v", args, got)
		}
	}
}

func TestCommandPlainIgnore(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		strings.Replace(commandBadWorkflow, "missing.value }}'", "missing.value }}' #ignore expression-reference", 1),
		strings.Replace(commandBadWorkflow, "      - run:", "      # ignore-next-line expression-reference\n      - run:", 1),
	} {
		if got := testRunCommand(input, "--no-config", "-"); got != (commandTranscript{}) {
			t.Fatalf("plain ignore did not suppress its finding: %+v", got)
		}
	}
}

func TestRootAndCheckArgumentParity(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-qv", "-"},
		{"-qojson", "-"},
		{"-qvo", "json", "-"},
		{"-q=false", "--oneline", "-"},
		{"-stdin-filename", "check", "--json", "-"},
		{"--stdin-filename", "--json", "-"},
		{"-ignore", "--json", "-"},
		{"--ignore-regex", "undefined variable", "-ignore", "other", "-"},
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
		{"./version", "completion"},
	} {
		got := testRunCommand("", args...)
		if got != (commandTranscript{}) {
			t.Errorf("%q: %+v", args, got)
		}
	}
}

func TestRootOptionsAfterInput(t *testing.T) {
	t.Parallel()
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
