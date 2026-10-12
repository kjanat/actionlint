package cli

import (
	"os"
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
		{"long", []string{"--oneline", "-"}, 1, "[expression]"},
		{"legacy", []string{"-oneline", "-"}, 1, "[expression]"},
		{"short format", []string{"-f", "{{range .}}{{.Kind}}{{end}}", "-"}, 1, "expression"},
		{"short output", []string{"-o", "json", "-"}, 1, `"rule":"expression"`},
		{"option-like value", []string{"--stdin-filename", "-completions", "-"}, 1, "-completions:6:"},
		{"option-like equal value", []string{"-stdin-filename=-completions", "-"}, 1, "-completions:6:"},
		{"ignore literal flag", []string{"-ignore", "--json", "-"}, 1, "[expression]"},
		{"true value", []string{"--oneline=TRUE", "-"}, 1, "[expression]"},
		{"unknown", []string{"--unknown"}, 2, "flag provided but not defined"},
		{"missing value", []string{"--output"}, 2, "needs an argument"},
		{"invalid bool", []string{"--oneline=maybe"}, 2, "invalid boolean value"},
		{"unknown shell", []string{"--completion", "not-a-shell"}, 2, "shell"},
		{"help exits early", []string{"--help", "--unknown"}, 0, "Usage:"},
		{"legacy help value", []string{"-help=false"}, 0, "Usage:"},
		{"help short", []string{"-h"}, 0, "Usage:"},
		{"invalid before help", []string{"--unknown", "--help"}, 2, "flag provided but not defined"},
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

func TestCommandLiteralFilenames(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"first.yml", "--json", "-completions", "--help", "version", "completion", "__complete"} {
		if err := os.WriteFile(name, []byte(commandGoodWorkflow), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"first.yml", "--json", "-completions", "--help"},
		{"--", "--json", "--help", "-completions", "__complete"},
		{"version", "completion"},
	} {
		got := testRunCommand("", args...)
		if got != (commandTranscript{}) {
			t.Errorf("%q: %+v", args, got)
		}
	}
}
