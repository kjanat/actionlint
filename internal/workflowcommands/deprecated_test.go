package workflowcommands_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/workflowcommands"
	"github.com/google/go-cmp/cmp"
)

func TestDeprecatedReplacementsAppendToQuotedDestination(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to execute replacement snippets")
	}
	for _, tc := range []struct {
		command, variable, want string
	}{
		{"::set-output name=foo::42", "GITHUB_OUTPUT", "foo=42\n"},
		{"::save-state name=foo::42", "GITHUB_STATE", "foo=42\n"},
		{"::set-env name=foo::42", "GITHUB_ENV", "foo=42\n"},
		{"::add-path::/path/to/foo", "GITHUB_PATH", "/path/to/foo\n"},
	} {
		t.Run(tc.variable, func(t *testing.T) {
			dir := t.TempDir()
			name := "command file [1]"
			destination := filepath.Join(dir, name)
			if err := os.WriteFile(destination, []byte("existing\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			findings := workflowcommands.Deprecated(tc.command)
			if len(findings) != 1 {
				t.Fatalf("got %d findings, want 1", len(findings))
			}
			script := strings.NewReplacer("{name}", "foo", "{value}", "42", "{path}", "/path/to/foo").Replace(findings[0].Replacement)
			cmd := exec.CommandContext(t.Context(), bash, "--noprofile", "--norc", "-c", script)
			cmd.Dir = dir
			t.Setenv(tc.variable, name)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("replacement failed: %v\n%s", err, output)
			}
			content, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			if want := "existing\n" + tc.want; string(content) != want {
				t.Fatalf("destination contains %q, want %q", content, want)
			}
		})
	}
}

func TestRuleDeprecatedCommandsDetectTargetCommands(t *testing.T) {
	tests := []struct {
		what string
		run  string
		want []string
	}{
		{
			what: "save-state",
			run:  "::save-state name=foo::42",
			want: []string{"save-state"},
		},
		{
			what: "set-output",
			run:  "::set-output name=foo::42",
			want: []string{"set-output"},
		},
		{
			what: "set-env",
			run:  "::set-env name=foo::42",
			want: []string{"set-env"},
		},
		{
			what: "add-path",
			run:  "::add-path::/path/to/foo",
			want: []string{"add-path"},
		},
		{
			what: "submatch",
			run:  "hello::set-output name=foo::42 world",
			want: []string{"set-output"},
		},
		{
			what: "multiple same commands",
			run:  "::set-output name=foo::42 ::set-output name=bar::xxx",
			want: []string{"set-output", "set-output"},
		},
		{
			what: "multiple different commands",
			run:  "::set-output name=foo::42 ::add-path::/path/to/foo ::save-state name=foo::42",
			want: []string{"set-output", "add-path", "save-state"},
		},
		{
			what: "multiple submatches",
			run:  "hello::set-output name=foo::42 how ::add-path::/path/to/foo are ::save-state name=foo::42 you",
			want: []string{"set-output", "add-path", "save-state"},
		},
		{
			what: "something between command and arguments",
			run:  "::set-output hello name=foo::42",
			want: []string{},
		},
		{
			what: "multiple spaces",
			run:  "::set-output    		    name=foo::42",
			want: []string{"set-output"},
		},
		{
			what: "empty string",
			run:  "",
			want: []string{},
		},
		{
			what: "no command",
			run:  "echo 'do not use set-output!'",
			want: []string{},
		},
		{
			what: "hyphen and underscore in name",
			run:  "::save-state name=foo_bar-woo::42",
			want: []string{"save-state"},
		},
		{
			what: "invalid name",
			run:  "::save-state name=-foo::42",
			want: []string{},
		},
		{
			what: "different argument",
			run:  "::save-state myname=foo::42",
			want: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			have := []string{}
			for _, finding := range workflowcommands.Deprecated(tc.run) {
				have = append(have, finding.Command)
				if got := tc.run[finding.Start:finding.End]; got != finding.Command {
					t.Fatalf("range contains %q", got)
				}
			}

			if diff := cmp.Diff(have, tc.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
