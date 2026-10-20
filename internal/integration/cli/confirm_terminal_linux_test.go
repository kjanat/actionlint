//go:build linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestReportOverwriteConfirmation(t *testing.T) {
	for _, modern := range []bool{false, true} {
		for _, tc := range []struct {
			name, answer, flag string
			stdinWorkflow      bool
			wantOverwrite      bool
			wantPrompt         bool
		}{
			{name: "yes", answer: "y\n", wantOverwrite: true, wantPrompt: true},
			{name: "piped yes", answer: "yes\nyes\n", wantOverwrite: true, wantPrompt: true},
			{name: "uppercase", answer: "YES\n", wantOverwrite: true, wantPrompt: true},
			{name: "no", answer: "n\n", wantPrompt: true},
			{name: "default", answer: "\n", wantPrompt: true},
			{name: "eof", wantPrompt: true},
			{name: "unrecognized", answer: "true\n", wantPrompt: true},
			{name: "long flag", flag: "--yes", wantOverwrite: true},
			{name: "short flag", flag: "-y", wantOverwrite: true},
			{name: "explicit false", flag: "--yes=false", answer: "n\n", wantPrompt: true},
			{name: "workflow stdin", answer: commandGoodWorkflow, stdinWorkflow: true},
			{name: "workflow stdin confirmed", flag: "-y", answer: commandGoodWorkflow, stdinWorkflow: true, wantOverwrite: true},
		} {
			t.Run(fmtTestName(modern, tc.name), func(t *testing.T) {
				t.Chdir(t.TempDir())
				if err := os.WriteFile("input.yml", []byte(commandGoodWorkflow), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile("report.json", []byte("previous report"), 0600); err != nil {
					t.Fatal(err)
				}
				terminal, finish := commandTestTerminal(t)
				var input io.Reader = commandFailingIO{} // --yes must not read for confirmation.
				var pipe *os.File
				if tc.flag == "" || !tc.wantOverwrite || tc.stdinWorkflow {
					reader, writer, err := os.Pipe()
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = reader.Close() })
					if _, err := io.WriteString(writer, tc.answer); err != nil {
						t.Fatal(err)
					}
					_ = writer.Close()
					input, pipe = reader, reader
				}
				var stdout bytes.Buffer
				cmd := cli.Command{Stdin: input, Stdout: &stdout, Stderr: terminal}
				args := []string{"actionlint"}
				if modern {
					args = append(args, "check")
				}
				args = append(args, "--shellcheck=", "--ruff=", "--json", "--output-file=report.json")
				if tc.flag != "" {
					args = append(args, tc.flag)
				}
				if tc.stdinWorkflow {
					args = append(args, "-")
				} else {
					args = append(args, "input.yml")
				}
				status := cmd.Main(args)
				stderr := finish()
				if strings.Contains(stderr, "Overwrite? [y/N]") != tc.wantPrompt {
					t.Fatalf("unexpected confirmation: %s", stderr)
				}
				data, err := os.ReadFile("report.json")
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantOverwrite {
					if status != 0 || !json.Valid(data) || stdout.Len() != 0 {
						t.Fatalf("confirmation failed: status=%d stderr=%s report=%s stdout=%s", status, stderr, data, &stdout)
					}
				} else {
					if status != 3 || string(data) != "previous report" {
						t.Fatalf("report changed without approval: status=%d stderr=%s report=%s", status, stderr, data)
					}
					if tc.stdinWorkflow {
						remaining, err := io.ReadAll(pipe)
						if err != nil || string(remaining) != tc.answer {
							t.Fatalf("confirmation consumed workflow input: %q, %v", remaining, err)
						}
					}
				}
			})
		}
	}
}
