//go:build linux

package cli_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestSummaryTerminalColor(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("ACTIONLINT_SUMMARY", "true")
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, tc := range []struct {
		name    string
		flags   []string
		colored bool
	}{
		{name: "automatic", colored: true},
		{name: "disabled", flags: []string{"--no-color"}},
		{name: "forced", flags: []string{"--color"}, colored: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terminal, finish := commandTestTerminal(t)
			var stdout bytes.Buffer
			cmd := cli.Command{Stdin: strings.NewReader(commandGoodWorkflow), Stdout: &stdout, Stderr: terminal}
			args := append([]string{"actionlint", "--no-config", "--no-tool=shellcheck", "--no-tool=ruff"}, tc.flags...)
			status := cmd.Main(append(args, "-"))
			output := strings.ReplaceAll(finish(), "\r", "")
			if status != 0 || ansi.MatchString(output) != tc.colored || ansi.ReplaceAllString(output, "") != "✓ Checked 1 workflow. All clear!\n" {
				t.Fatalf("summary styling: exit=%d stderr=%q, want colored=%v", status, output, tc.colored)
			}
		})
	}
}
