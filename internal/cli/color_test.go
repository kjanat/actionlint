package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func TestGitHubActionsColor(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("FORCE_HYPERLINKS", "")
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, tc := range []struct {
		name, actions, noColor string
		flags                  []string
		colored                bool
	}{
		{name: "actions logs", actions: "true", colored: true},
		{name: "outside actions"},
		{name: "false", actions: "false"},
		{name: "one", actions: "1"},
		{name: "no color", actions: "true", noColor: "1"},
		{name: "nonempty no color", actions: "true", noColor: "0"},
		{name: "forced", actions: "true", noColor: "1", flags: []string{"--color"}, colored: true},
		{name: "disabled", actions: "true", flags: []string{"--no-color"}},
		{name: "disabled beats forced", actions: "true", flags: []string{"--no-color", "--color"}},
		{name: "false retains auto", actions: "true", flags: []string{"--color=false"}, colored: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_ACTIONS", tc.actions)
			t.Setenv("NO_COLOR", tc.noColor)
			for _, modern := range []bool{false, true} {
				for _, help := range []bool{false, true} {
					t.Run(fmt.Sprintf("modern=%t/help=%t", modern, help), func(t *testing.T) {
						previous := color.NoColor
						t.Cleanup(func() { color.NoColor = previous })
						color.NoColor = true
						args := append([]string{"actionlint", "--shellcheck=", "--pyflakes="}, tc.flags...)
						if modern {
							args = append(args, "check")
						}
						want := 1
						if help {
							args = append(args, "--help")
							want = 0
						} else {
							args = append(args, "-")
						}
						var out, stderr bytes.Buffer
						cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: &out, Stderr: &stderr}
						if code := cmd.Main(args); code != want {
							t.Fatalf("exit = %d, want %d: %s", code, want, &stderr)
						}
						result, other := out.String(), stderr.String()
						if help {
							result, other = other, result
						}
						if result == "" || other != "" || ansi.MatchString(result) != tc.colored || strings.Contains(result, "\x1b]8;") {
							t.Fatalf("colored = %t, want %t; stdout %q, stderr %q", ansi.MatchString(result), tc.colored, &out, &stderr)
						}
					})
				}
			}
		})
	}
}

func TestGitHubActionsPlainOutput(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("NO_COLOR", "")
	previous := color.NoColor
	t.Cleanup(func() { color.NoColor = previous })
	for _, args := range [][]string{
		{"--version"}, {"version"}, {"version", "--json"},
		{"--json", "--help"}, {"check", "--json", "--help"},
		{"check", "--color=never", "--help"}, {"check", "--color=never", "-"},
		{"--json", "-"}, {"check", "--json", "-"},
		{"check", "--output-format=jsonl", "-"},
		{"check", "--output-format=sarif", "-"},
		{"check", "--output-format=github", "-"},
		{"-format", "{{range .}}{{.Message}}\n{{end}}", "-"},
		{"check", "--template", "{{range .}}{{.Message}}\n{{end}}", "-"},
		{"completion", "bash"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			color.NoColor = true
			var out, stderr bytes.Buffer
			cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: &out, Stderr: &stderr}
			code := cmd.Main(append([]string{"actionlint", "--shellcheck=", "--pyflakes="}, args...))
			if code > 1 || out.Len()+stderr.Len() == 0 || strings.ContainsRune(out.String()+stderr.String(), '\x1b') {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, &out, &stderr)
			}
		})
	}
}

func TestGitHubActionsOutputDestination(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_HYPERLINKS", "")
	for _, args := range [][]string{{"--help"}, {"-"}, {"check", "--help"}, {"check", "--output-format=oneline", "-"}} {
		for _, pipe := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pipe=%t", strings.Join(args, " "), pipe), func(t *testing.T) {
				previous := color.NoColor
				t.Cleanup(func() { color.NoColor = previous })
				color.NoColor = true
				var read, write *os.File
				var err error
				if pipe {
					read, write, err = os.Pipe()
				} else {
					write, err = os.Create(filepath.Join(t.TempDir(), "output.txt"))
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = write.Close() })
				var data []byte
				done := make(chan struct{})
				if pipe {
					t.Cleanup(func() { _ = read.Close() })
					go func() {
						data, err = io.ReadAll(read)
						close(done)
					}()
				}
				cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: write, Stderr: write}
				code := cmd.Main(append([]string{"actionlint", "--shellcheck=", "--pyflakes="}, args...))
				if e := write.Close(); e != nil {
					t.Fatal(e)
				}
				if pipe {
					<-done
				} else {
					data, err = os.ReadFile(write.Name())
				}
				if err != nil || code > 1 || len(data) == 0 || bytes.Contains(data, []byte("\x1b[")) != pipe {
					t.Fatalf("pipe %t, exit %d, error %v, output %q", pipe, code, err, data)
				}
			})
		}
	}
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("report/forced=%t", force), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.txt")
			args := []string{"check", "--output-file", path}
			if force {
				args = append(args, "--color=always")
			}
			got := testRunCommand(commandBadWorkflow, append(args, "--no-color=false", "-")...)
			data, err := os.ReadFile(path)
			if err != nil || got.Status != 1 || got.Stdout != "" || got.Stderr != "" || bytes.Contains(data, []byte("\x1b[")) != force {
				t.Fatalf("forced %t: %+v, output %q (%v)", force, got, data, err)
			}
		})
	}
}
