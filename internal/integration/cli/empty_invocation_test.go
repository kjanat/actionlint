package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestBareInvocationShowsHelp(t *testing.T) {
	for _, repository := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside repository", true: "repository with findings"}[repository], func(t *testing.T) {
			if repository {
				commandTestRepo(t)
			} else {
				t.Chdir(t.TempDir())
			}
			var stdout, stderr bytes.Buffer
			cmd := cli.Command{Stdout: &stdout, Stderr: &stderr}
			status := cmd.MainContext(t.Context(), []string{"actionlint"})
			if status != 0 || !strings.Contains(stdout.String(), "Usage:") || stderr.Len() != 0 {
				t.Fatalf("status=%d stdout=%s stderr=%s", status, &stdout, &stderr)
			}
		})
	}
}

func TestCheckWithoutInputsFails(t *testing.T) {
	for _, layout := range []string{"outside repository", "empty repository", "empty workflows"} {
		t.Run(layout, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if layout != "outside repository" {
				gitRepository(t, ".")
			}
			if layout == "empty workflows" {
				if err := os.MkdirAll(".github/workflows", 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, format := range []string{"text", "json"} {
				got := testRunCommand("", "check", "--output-format="+format)
				if got.Status != 3 || got.Stdout != "" || got.Stderr == "" {
					t.Fatalf("%s: %+v", format, got)
				}
				if format == "json" {
					var result struct {
						Status    string
						Completed bool
						ExitCode  int `json:"exit_code"`
					}
					if err := json.Unmarshal([]byte(got.Stderr), &result); err != nil {
						t.Fatal(err)
					}
					if result.Status != "failure" || result.Completed || result.ExitCode != 3 {
						t.Fatalf("%+v", result)
					}
				}
			}
		})
	}
}
