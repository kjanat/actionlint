package actionlint

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestShellcheckShellStartup(t *testing.T) {
	for _, tc := range []struct {
		name, setup string
		implicit    bool
	}{
		{"bash", "set -e", true},
		{"bash", "set -e -o pipefail", false},
		{"BASH", "set -e -o pipefail", false},
		{"sh", "set -e", true},
		{"bash {0}", "", false},
		{"bash -e {0}", "set -e", false},
		{"bash -eu -o pipefail {0}", "set -eu -o pipefail", false},
		{"bash -euxo pipefail {0}", "set -eux -o pipefail", false},
		{"/usr/bin/bash -euxo pipefail {0}", "set -eux -o pipefail", false},
		{`C:\tools\bash.exe -eo pipefail {0}`, "set -e -o pipefail", false},
		{"dash -eu {0}", "set -eu", false},
		{"ksh -eu {0}", "set -eu", false},
		{"bash -euo pipefail +e +o pipefail {0}", "set -u", false},
		{"bash -o errexit +e {0}", "", false},
		{"bash -e +o errexit {0}", "", false},
		{"bash +e -o errexit {0}", "set -e", false},
		{"bash -e -- {0}", "set -e", false},
		{"bash -o noglob {0}", "set -f", false},
		{"bash -o future-option {0}", "", false},
		{"bash -e +e {0}", "", false},
		{"bash -o 'pipefail' {0}", "", false},
		{"bash -o \"pipefail\" {0}", "", false},
		{"bash ${{ vars.FLAGS }} {0}", "", false},
		{"bash --rcfile custom {0}", "", false},
		{"bash {0} -e", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dialect, setup := (shellcheckShell{name: tc.name, implicit: tc.implicit}).analysis()
			if dialect == "" || setup != tc.setup {
				t.Fatalf("got %q / %q, want setup %q", dialect, setup, tc.setup)
			}
		})
	}
}

func TestShellcheckUnknownScriptLanguage(t *testing.T) {
	for _, shell := range []string{
		"bash -c 'python {0}'", "bash -ec 'python {0}'", "bash -s {0}",
		`bash "-c" "python {0}"`, "python {0}", "pwsh {0}",
		"actions-shell python {0}", "wrapper bash {0}", "bash other-script {0}", "bash -- other-script {0}",
	} {
		t.Run(shell, func(t *testing.T) {
			dialect, setup := (shellcheckShell{name: shell}).analysis()
			if dialect != "" || setup != "" {
				t.Fatalf("unknown script language inferred as %q / %q", dialect, setup)
			}
		})
	}
}

func TestShellcheckResolvedDiagnostics(t *testing.T) {
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	for _, tc := range []struct {
		name, container, shell string
		finding                bool
	}{
		{"host Bash", "", "", false},
		{"container sh", "alpine", "", true},
		{"container override", "alpine", "bash", false},
		{"container explicit sh", "alpine", "sh", true},
		{"empty container", "''", "", false},
		{"known container expression", "${{ 'alpine' }}", "", true},
		{"known container mapping", `${{ fromJSON('{"image":"alpine"}') }}`, "", true},
		{"empty container mapping", `${{ fromJSON('{"image":""}') }}`, "", false},
		{"unknown container", "${{ vars.IMAGE }}", "", false},
		{"override unknown container", "${{ vars.IMAGE }}", "sh", true},
		{"literal shell expression", "", "${{ 'sh' }}", true},
		{"unknown shell", "alpine", "${{ vars.SHELL }}", false},
		{"custom sh", "", "sh {0}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
			if tc.container != "" {
				source += "    container: " + tc.container + "\n"
			}
			source += "    steps:\n      - run: |\n          [[ -n \"$HOME\" ]]\n"
			line := strings.Count(source, "\n")
			if tc.shell != "" {
				source += "        shell: " + tc.shell + "\n"
			}
			// A following host job must not inherit container or step shell state.
			source += "  next:\n    runs-on: ubuntu-latest\n    steps:\n      - run: '[[ -n \"$HOME\" ]]'\n"
			linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: command, WorkingDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			findings, err := linter.Lint("workflow.yml", []byte(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			var shellFindings []*Error
			for _, finding := range findings {
				if finding.Kind == "shellcheck" {
					shellFindings = append(shellFindings, finding)
				}
			}
			if !tc.finding && len(shellFindings) == 0 {
				return
			}
			if !tc.finding || len(shellFindings) != 1 || !strings.Contains(shellFindings[0].Message, "SC3010") || shellFindings[0].Line != line || shellFindings[0].Column != 11 {
				t.Fatalf("expected bashism=%v at %d:11; got %v", tc.finding, line, shellFindings)
			}
		})
	}
}
