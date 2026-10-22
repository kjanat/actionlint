package shellcheck

import (
	"testing"
)

func TestShellcheckDialectOption(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-xs", "sh"}, "sh"},
		{[]string{"--shell", "dash"}, "dash"},
		{[]string{"--shell=bash", "-sksh"}, "ksh"},
		{[]string{"--source-path", "--shell=sh"}, ""},
		{[]string{"-P", "-ssh"}, ""},
		{[]string{"-P-ssh"}, ""},
		{[]string{"--", "--shell=sh"}, ""},
		{[]string{"-Cnever", "-ssh"}, "sh"},
	} {
		dialect, found := shellcheckDialectOption(tc.args)
		if dialect != tc.want || found != (tc.want != "") {
			t.Errorf("%q: got %q/%t, want %q", tc.args, dialect, found, tc.want)
		}
	}
}

func TestShellcheckDialectEnvironmentOverride(t *testing.T) {
	t.Setenv("SHELLCHECK_OPTS", "--shell=sh")
	cmd := externalCommand{Env: []string{"SHELLCHECK_OPTS="}}
	if dialect, found := shellcheckDialect(&cmd); found {
		t.Fatalf("empty child override retained parent dialect %q", dialect)
	}
}

func TestShellcheckHeaderSelection(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   bool
	}{
		{"# shellcheck shell=bash\necho ok\n", true},
		{"# shellcheck disable=SC2086 shell='sh'\necho ok\n", true},
		{"# shellcheck source-path=\"directory with spaces\" shell=bash\n", true},
		{"# shellcheck source-path=\"directory shell=bash\"\n", false},
		{"# shellcheck disable=SC2086 # shell=bash\n", false},
		{"echo ok\n# shellcheck shell=bash\n", false},
		{"# Example: shellcheck shell=bash\n", false},
	} {
		_, _, selected := shellcheckHeader(tc.script)
		if selected != tc.want {
			t.Errorf("%q: selected=%t, want %t", tc.script, selected, tc.want)
		}
	}
}
