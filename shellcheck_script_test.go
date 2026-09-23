package actionlint

import (
	"strings"
	"testing"
)

func TestShellcheckScriptSemantics(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, shell, script, env, config string
		settings                         string
		args                             []string
		code                             string
		line                             int
	}{
		{name: "file directive covers both commands", shell: "bash", script: "# shellcheck disable=SC2086\necho $HOME\necho $HOME"},
		{name: "shebang then file directive", shell: "bash", script: "#!/usr/bin/env bash\n# shellcheck disable=SC2086\necho $HOME\necho $HOME"},
		{name: "command directive stays local", shell: "bash", script: ":\n# shellcheck disable=SC2086\necho $HOME\necho $HOME", code: "SC2086", line: 4},
		{name: "native dialect beats inferred", shell: "sh", script: "# shellcheck shell=bash\n[[ -n \"$HOME\" ]]"},
		{name: "unknown wrapper explicit selection", shell: "custom-shell {0}", script: "# shellcheck shell=sh\n[[ -n \"$HOME\" ]]", code: "SC3010", line: 2},
		{name: "multiple native selectors", shell: "custom-shell {0}", script: "# shellcheck disable=SC2086 shell='sh'\n[[ -n \"$HOME\" ]]", code: "SC3010", line: 2},
		{name: "wrapper not guessed from arguments", shell: "actions-shell python {0}", script: "import os\nprint(os.getcwd())"},
		{name: "command mode not guessed", shell: "bash -c 'python {0}'", script: "import os\nprint(os.getcwd())"},
		{name: "path selects dialect", shell: "/usr/bin/dash {0}", script: "[[ -n \"$HOME\" ]]", code: "SC3010", line: 1},
		{name: "bundled errexit", shell: "bash -euxo pipefail {0}", script: "cd missing\nrm file"},
		{name: "matching native dialect retains errexit", shell: "bash -e {0}", script: "# shellcheck shell=bash\ncd missing\nrm file"},
		{name: "quoted native dialect retains errexit", shell: "bash -e {0}", script: "# shellcheck disable=SC2086 shell='bash'\ncd missing\nrm file"},
		{name: "first native dialect retains errexit", shell: "bash -e {0}", script: "# shellcheck shell=bash shell=sh\ncd missing\nrm file"},
		{name: "different native dialect clears startup", shell: "bash -e -o pipefail {0}", script: "# shellcheck shell=sh\ncd missing\nrm file", code: "SC2164", line: 2},
		{name: "matching native dialect retains pipefail", shell: "bash -o pipefail {0}", script: "# shellcheck shell=\"bash\"\nfalse | true", args: []string{"--enable=check-extra-masked-returns"}},
		{name: "later flag disables errexit", shell: "bash -euxo pipefail +e {0}", script: "cd missing\nrm file", code: "SC2164", line: 1},
		{name: "pipefail affects analysis", shell: "bash -euxo pipefail {0}", script: "false | true", args: []string{"--enable=check-extra-masked-returns"}},
		{name: "later flag disables pipefail", shell: "bash -euxo pipefail +o pipefail {0}", script: "false | true", args: []string{"--enable=check-extra-masked-returns"}, code: "SC2312", line: 1},
		{name: "script flags participate in native analysis", shell: "bash {0}", script: "set -e\ncd missing\nrm file"},
		{name: "explicit dialect argument", shell: "bash", script: "[[ -n \"$HOME\" ]]", args: []string{"-s", "sh"}, code: "SC3010", line: 1},
		{name: "attached short dialect argument", shell: "bash", script: "[[ -n \"$HOME\" ]]", args: []string{"-ssh"}, code: "SC3010", line: 1},
		{name: "environment dialect", shell: "bash", script: "[[ -n \"$HOME\" ]]", env: "--shell=sh", code: "SC3010", line: 1},
		{name: "arguments override environment", shell: "bash", script: "[[ -n \"$HOME\" ]]", env: "--shell=sh", args: []string{"--shell=bash"}},
		{name: "flag overrides directive", shell: "sh", script: "# shellcheck shell=bash\n[[ -n \"$HOME\" ]]", args: []string{"--shell=sh"}, code: "SC3010", line: 2},
		{name: "config overrides inferred dialect", shell: "bash", script: "[[ -n \"$HOME\" ]]", config: "shell: sh", code: "SC3010", line: 1},
		{name: "directive overrides config", shell: "sh", script: "# shellcheck shell=bash\n[[ -n \"$HOME\" ]]", config: "shell: sh"},
		{name: "directive preserves sh diagnostics", shell: "bash", script: "# shellcheck shell=sh\n[[ -n \"$HOME\" ]]", config: "shell: bash", code: "SC3010", line: 2},
		{name: "flag overrides config", shell: "bash", script: "[[ -n \"$HOME\" ]]", args: []string{"--shell=bash"}, config: "shell: sh"},
		{name: "environment overrides config", shell: "sh", script: "[[ -n \"$HOME\" ]]", env: "--shell=bash", config: "shell: sh"},
		{name: "flag overrides environment and config", shell: "bash", script: "[[ -n \"$HOME\" ]]", env: "--shell=sh", args: []string{"--shell=bash"}, config: "shell: sh"},
		{name: "settings override inferred and project dialect", shell: "sh", script: "[[ -n \"$HOME\" ]]", config: "shell: sh", settings: "bash"},
		{name: "directive overrides settings", shell: "sh", script: "# shellcheck shell=bash\n[[ -n \"$HOME\" ]]", settings: "sh"},
		{name: "flag overrides settings", shell: "sh", script: "[[ -n \"$HOME\" ]]", args: []string{"-s", "bash"}, settings: "sh"},
		{name: "environment overrides settings", shell: "sh", script: "[[ -n \"$HOME\" ]]", env: "--shell=bash", settings: "sh"},
		{name: "config and user header combine", shell: "bash", script: "#!/bin/bash\n# shellcheck disable=SC2086\necho $HOME\necho $HOME", config: "disable: [SC2016]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHELLCHECK_OPTS", tc.env)
			options := AnalysisOptions{
				WorkingDir: t.TempDir(), StdinFileName: "workflow.yml", Shellcheck: command,
				ShellcheckOptions: &ExternalCommandOptions{Arguments: tc.args},
			}
			if tc.config != "" {
				options.ConfigFile = writeShellcheckFixture(t, options.WorkingDir, "actionlint.yaml", "tools: {shellcheck: {config: {"+tc.config+"}}}\n")
			}
			if tc.settings != "" {
				options.ShellcheckSettings = &ShellcheckSettings{Shell: tc.settings}
			}
			session, err := NewAnalysisSession(options)
			if err != nil {
				t.Fatal(err)
			}
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          " + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n        shell: " + tc.shell + "\n"
			result, err := session.ReadStdin(strings.NewReader(workflow), false)
			if err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want one %s finding, got %+v", tc.code, result.Diagnostics)
			}
			finding := result.Diagnostics[0]
			if finding.Rule != "shellcheck" || !strings.Contains(finding.Message, tc.code) || finding.Start.Line != 6+tc.line {
				t.Fatalf("want %s on workflow line %d; got %+v", tc.code, 6+tc.line, finding)
			}
		})
	}
}

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
	cmd := externalCommand{env: []string{"SHELLCHECK_OPTS="}}
	if dialect, found := cmd.shellcheckDialect(); found {
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
