package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestSummaryDefaultsAndOverrides(t *testing.T) {
	t.Setenv("ACTIONLINT_SUMMARY", "")
	if err := os.Unsetenv("ACTIONLINT_SUMMARY"); err != nil {
		t.Fatal(err)
	}
	for _, modern := range []bool{false, true} {
		for _, tc := range []struct {
			name, input, env, want string
			flags                  []string
			status                 int
		}{
			{name: "clean", input: commandGoodWorkflow, want: "✓ Checked 1 file. All clear!\n"},
			{name: "findings", input: commandBadWorkflow, want: "! Checked 1 file, 1 finding.\n\nFiles:\n  1  <stdin>\n\nDiagnostics:\n  1  expression-reference/unknown-variable\n", status: 1},
			{name: "quiet", input: commandGoodWorkflow, flags: []string{"--quiet"}},
			{name: "disabled", input: commandGoodWorkflow, flags: []string{"--summary=false"}},
			{name: "json", input: commandGoodWorkflow, flags: []string{"--json"}, want: "{\"summary\":{\"files\":1,\"findings\":0}}\n"},
			{name: "template", input: commandGoodWorkflow, flags: []string{"--template={{range .}}{{.Kind}}{{end}}"}, want: "✓ Checked 1 file. All clear!\n"},
			{name: "environment disabled", input: commandGoodWorkflow, env: "false"},
			{name: "flag overrides environment", input: commandGoodWorkflow, env: "true", flags: []string{"--summary=false"}},
		} {
			t.Run(fmtTestName(modern, tc.name), func(t *testing.T) {
				if tc.env != "" {
					t.Setenv("ACTIONLINT_SUMMARY", tc.env)
				}
				var stdout, stderr bytes.Buffer
				cmd := cli.Command{Stdin: strings.NewReader(tc.input), Stdout: &stdout, Stderr: &stderr}
				args := []string{"actionlint", "--no-tool=shellcheck", "--no-tool=ruff", "--no-config", "--no-color"}
				if modern {
					args = append(args, "check")
				}
				args = append(args, tc.flags...)
				status := cmd.Main(append(args, "-"))
				output := stderr.String()
				if status != tc.status || output != tc.want {
					t.Fatalf("summary status=%d stderr=%q, want status=%d stderr=%q", status, output, tc.status, tc.want)
				}
			})
		}
	}
}

func TestActionCodesAndSummaryThroughCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one", "action.yml"), filepath.Join(dir, "two", "action.yml")
	for path, source := range map[string]string{
		one: "name: example\ndescription: example\nruns: {using: node20, main: missing.js}\n",
		two: "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok}]}\n",
	} {
		if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := testRunCommand("", "--no-config", "--summary", one, two)
	if got.Status != 1 || !strings.Contains(got.Stdout, "error[action] missing-file") ||
		!strings.Contains(got.Stdout, "error[action] runtime-deprecated") || !strings.Contains(got.Stdout, "error[action] missing-shell") {
		t.Fatalf("specific action diagnostics: %+v", got)
	}
	want := "! Checked 2 files, 3 findings.\n\nFiles:\n  2  " + physicalWorkingDirectoryPath(t, one) + "\n  1  " + physicalWorkingDirectoryPath(t, two) +
		"\n\nDiagnostics:\n  1  action/missing-file\n  1  action/missing-shell\n  1  action/runtime-deprecated\n"
	if got.Stderr != want {
		t.Fatalf("summary = %q, want %q", got.Stderr, want)
	}
}

func TestHelpOutputDefaults(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"check", "--help"}} {
		got := testRunCommand("", args...)
		if got.Status != 0 || !strings.Contains(got.Stderr, "default pretty") {
			t.Fatalf("output default in help: %+v", got)
		}
	}
}
