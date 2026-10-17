package adapters

import (
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
