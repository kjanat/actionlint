package cli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/cli"
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
			{name: "clean", input: commandGoodWorkflow, want: "✓ Checked 1 workflow. All clear!\n"},
			{name: "findings", input: commandBadWorkflow, want: "! Checked 1 workflow, 1 finding.\n", status: 1},
			{name: "quiet", input: commandGoodWorkflow, flags: []string{"--quiet"}},
			{name: "disabled", input: commandGoodWorkflow, flags: []string{"--summary=false"}},
			{name: "json", input: commandGoodWorkflow, flags: []string{"--json"}, want: "{\"summary\":{\"files\":1,\"findings\":0}}\n"},
			{name: "template", input: commandGoodWorkflow, flags: []string{"--template={{range .}}{{.Kind}}{{end}}"}, want: "✓ Checked 1 workflow. All clear!\n"},
			{name: "environment disabled", input: commandGoodWorkflow, env: "false"},
			{name: "flag overrides environment", input: commandGoodWorkflow, env: "true", flags: []string{"--summary=false"}},
		} {
			t.Run(fmtTestName(modern, tc.name), func(t *testing.T) {
				if tc.env != "" {
					t.Setenv("ACTIONLINT_SUMMARY", tc.env)
				}
				var stdout, stderr bytes.Buffer
				cmd := cli.Command{Stdin: strings.NewReader(tc.input), Stdout: &stdout, Stderr: &stderr}
				args := []string{"actionlint", "--shellcheck=", "--ruff=", "--no-config", "--no-color"}
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
