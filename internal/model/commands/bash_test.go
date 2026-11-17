package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashInvocationMatchesActualBash(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash is required for the invocation reference")
	}
	for _, command := range []string{
		"bash -euo pipefail {0}", "bash -euo -pipefail {0}", "bash -oe pipefail {0}",
		"bash -o future-option {0}", "bash -o \"pipefail\" {0}", "bash -o 'pipefail' {0}",
		"bash -O extglob {0}", "bash +O extglob {0}", "bash -O definitely_not_a_shopt {0}",
		"bash --help -z {0}", "bash --help --invalid-after-help {0}", "bash --version --invalid-after-version {0}",
		"bash --noprofile --norc -eu {0}", "bash -e --noprofile {0}", "bash -Oextglob {0}",
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			invocation := BuiltinShells().AnalyzeFor(command, false, UnixArguments)
			var arguments []string
			for _, token := range invocation.Arguments {
				arguments = append(arguments, strings.ReplaceAll(token.Value, "{0}", "/dev/null"))
			}
			// Send argv as data. Windows MSYS argument parsing can otherwise strip
			// literal quotes before Bash's option parser receives the argument.
			process := exec.CommandContext(t.Context(), bash, "--noprofile", "--norc", "-c", `args=(); while IFS= read -r -d '' arg; do args+=("$arg"); done; exec bash "${args[@]}"`)
			process.Stdin = strings.NewReader(strings.Join(arguments, "\x00") + "\x00")
			output, err := process.CombinedOutput()
			if (invocation.Status == Valid) != (err == nil) {
				t.Fatalf("model=%+v; Bash error=%v output=%s", invocation, err, output)
			}
		})
	}
}

func TestWrapperDelegationAndCustomDiagnostics(t *testing.T) {
	t.Parallel()
	registry, err := NewWithShells([]Definition{
		{ID: "example/python", Executables: []string{"python"}, Language: "python", Options: []Option{{Short: "u"}}},
		{ID: "example/actions-python", Executables: []string{"actions-shell"}, Prefix: []string{"python"}, Delegate: "example/python"},
		{ID: "example/actions-bash", Executables: []string{"actions-shell"}, Prefix: []string{"bash"}, Delegate: "bash"},
		{ID: "example/custom", Executables: []string{"custom"}, Language: "javascript", Options: []Option{{Long: "unsafe", Warning: "This option bypasses the wrapper's sandbox."}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		command, language string
		status            Status
	}{
		{"actions-shell python -u {0}", "python", Valid},
		{"actions-shell bash -euo pipefail {0}", "bash", Valid},
		{"actions-shell bash -euo -pipefail {0}", "bash", Invalid},
		{"actions-shell rust {0}", "", Unknown},
	} {
		result := registry.Analyze(tc.command, false)
		if result.Language != tc.language || result.Status != tc.status {
			t.Errorf("%q: %+v", tc.command, result)
		}
	}
	result := registry.Analyze("custom --unsafe {0}", false)
	if len(result.Issues) != 1 || result.Issues[0].Severity != "warning" || result.Language != "javascript" {
		t.Fatalf("custom validator: %+v", result)
	}
}

func TestUnknownOptionsDoNotLookLikeValidatedInvocations(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"unregistered {0}", "bash ${{ vars.FLAGS }} {0}"} {
		result := BuiltinShells().Analyze(command, false)
		if result.Status != Unknown || result.Reason == "" {
			t.Fatalf("unresolved invocation reported as checked: %+v", result)
		}
	}
}

func TestBashPreparationMatchesStartupOptions(t *testing.T) {
	t.Parallel()
	executable, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash is required")
	}
	const snapshot = "set +o; shopt -p || :"
	for _, command := range []string{
		"bash -euo pipefail +e +o pipefail {0}",
		"bash +B {0}",
		"bash -O extglob -O nullglob +O extglob {0}",
		"bash -e -O extglob +e {0}",
		"bash -o posix +o posix {0}",
		"bash -o emacs -o vi +o emacs {0}",
		"bash -o posix +O inherit_errexit {0}",
		"bash -O inherit_errexit -o posix +o posix {0}",
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			if strings.Contains(command, "inherit_errexit") {
				probe := exec.CommandContext(t.Context(), executable, "--noprofile", "--norc", "-c", "shopt -s inherit_errexit")
				if err := probe.Run(); err != nil {
					t.Skip("this Bash version does not implement inherit_errexit")
				}
			}
			invocation := BuiltinShells().AnalyzeFor(command, false, UnixArguments)
			if invocation.Status != Valid {
				t.Fatalf("invalid invocation: %+v", invocation)
			}
			script := filepath.ToSlash(filepath.Join(t.TempDir(), "snapshot.sh"))
			if err := os.WriteFile(script, []byte(snapshot), 0600); err != nil {
				t.Fatal(err)
			}
			arguments := []string{"--noprofile", "--norc"}
			for _, token := range invocation.Arguments {
				arguments = append(arguments, strings.ReplaceAll(token.Value, "{0}", script))
			}
			original := exec.CommandContext(t.Context(), executable, "--noprofile", "--norc", "-c", `args=(); while IFS= read -r -d '' arg; do args+=("$arg"); done; exec bash "${args[@]}"`)
			original.Stdin = strings.NewReader(strings.Join(arguments, "\x00") + "\x00")
			want, err := original.CombinedOutput()
			if err != nil {
				t.Fatalf("original: %v: %s", err, want)
			}
			prepared := exec.CommandContext(t.Context(), executable, "--noprofile", "--norc", "-c", BashSetup(invocation.Settings)+"\n"+snapshot)
			got, err := prepared.CombinedOutput()
			if err != nil {
				t.Fatalf("prepared: %v: %s", err, got)
			}
			if string(got) != string(want) {
				t.Fatalf("setup %s\noriginal:\n%s\nprepared:\n%s", BashSetup(invocation.Settings), want, got)
			}
		})
	}
}
