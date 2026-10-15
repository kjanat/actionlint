package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	actionlint "actionlint.kjanat.dev/internal/engine"
	ruffcheck "actionlint.kjanat.dev/internal/ruff"
)

func TestRuffDefaultAndPyflakesSelection(t *testing.T) {
	ruff, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	// Keep tool-manager shims in the repository's version context. Both the
	// native command and actionlint disable configuration discovery below.
	for _, key := range []string{"ACTIONLINT_RUFF_BIN", "ACTIONLINT_RUFF_FLAGS", "ACTIONLINT_RUFF_ENV"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	var probeErr error
	if err := ruffcheck.CheckCompatibility(t.Context(), func(args []string, stdin string, callback func([]byte, error) error) {
		cmd := exec.CommandContext(t.Context(), ruff, args...)
		cmd.Stdin = strings.NewReader(stdin)
		output, err := cmd.Output()
		probeErr = callback(output, err)
	}, func() error { return probeErr }); err != nil {
		t.Skipf("Ruff integration requires a compatible executable (%s): %v", ruff, err)
	}
	script := "x = 1\nimport os\nif x == None:\n    print(missing)\n"
	nativeCodes := func(selection ...string) []string {
		t.Helper()
		args := append([]string{"check", "--isolated", "--no-fix", "--no-cache", "--output-format=json", "--stdin-filename=actionlint.py"}, selection...)
		cmd := exec.CommandContext(t.Context(), ruff, append(args, "-")...)
		cmd.Stdin = strings.NewReader(script)
		output, err := cmd.Output()
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			t.Fatalf("Ruff: %v", err)
		}
		var findings []struct{ Code string }
		if err := json.Unmarshal(output, &findings); err != nil {
			t.Fatal(err)
		}
		codes := make([]string, 0, len(findings))
		for _, finding := range findings {
			codes = append(codes, finding.Code)
		}
		slices.Sort(codes)
		return codes
	}
	defaults, pyflakes := nativeCodes(), nativeCodes("--select=F")
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: |\n          " + strings.ReplaceAll(strings.TrimSuffix(script, "\n"), "\n", "\n          ") + "\n"
	for _, prefix := range [][]string{nil, {"check"}} {
		for _, tc := range []struct {
			name  string
			flags []string
			want  []string
		}{
			{"defaults", nil, defaults},
			{"legacy Pyflakes", []string{"--pyflakes=old-command"}, pyflakes},
			{"single dash Pyflakes", []string{"-pyflakes", "old-command"}, pyflakes},
			{"explicit Ruff", []string{"--pyflakes=old-command", "--ruff"}, defaults},
			{"explicit Ruff first", []string{"--ruff", "--pyflakes=old-command"}, defaults},
			{"explicit Ruff command", []string{"--pyflakes=old-command", "--ruff=" + ruff}, defaults},
			{"disabled Ruff", []string{"--pyflakes=old-command", "--no-ruff"}, nil},
			{"empty Ruff", []string{"--pyflakes=old-command", "--ruff="}, nil},
			{"empty Pyflakes with Ruff", []string{"--pyflakes=", "--ruff"}, defaults},
			{"empty Pyflakes", []string{"--pyflakes="}, nil},
			{"disabled Pyflakes", []string{"--pyflakes=false"}, nil},
		} {
			if len(prefix) != 0 && tc.name == "single dash Pyflakes" {
				continue
			}
			t.Run(strings.Join(prefix, " ")+"/"+tc.name, func(t *testing.T) {
				args := append([]string{"actionlint", "--no-color", "--summary=false", "--no-config", "--shellcheck=", "--json"}, prefix...)
				args = append(args, tc.flags...)
				var stdout, stderr bytes.Buffer
				command := Command{Stdin: strings.NewReader(workflow), Stdout: &stdout, Stderr: &stderr}
				status := command.Main(append(args, "-"))
				if status > 1 {
					t.Fatalf("status=%d stdout=%s stderr=%s", status, &stdout, &stderr)
				}
				var result struct {
					Diagnostics []actionlint.Diagnostic `json:"diagnostics"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatalf("%v: %s", err, &stdout)
				}
				var got []string
				for _, finding := range result.Diagnostics {
					got = append(got, finding.Code)
				}
				slices.Sort(got)
				if !slices.Equal(got, tc.want) {
					t.Fatalf("codes=%v, want %v: stdout=%s stderr=%s", got, tc.want, &stdout, &stderr)
				}
			})
		}
	}
}

func TestDeprecatedPyflakesFlag(t *testing.T) {
	workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(undefined_name)
`
	for _, prefix := range [][]string{nil, {"check"}} {
		args := append(append([]string{}, prefix...), "--pyflakes=does-not-exist --invalid", "--json", "-")
		got := testRunCommand(workflow, args...)
		if got.Status != 0 || !strings.Contains(got.Stderr, "--pyflakes is deprecated") {
			t.Fatalf("%v: %+v", args, got)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(got.Stderr), "\n") {
			if !json.Valid([]byte(line)) {
				t.Fatalf("non-JSON warning: %s", line)
			}
		}
	}
}
