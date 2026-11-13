package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
)

func TestBareInvocationUsesRepositoryContext(t *testing.T) {
	for _, tc := range []struct {
		layout string
		status int
	}{
		{"outside repository", 0},
		{"repository with findings", 1},
		{"clean repository", 0},
		{"empty repository", 3},
		{"repository subdirectory", 1},
		{"file named check", 1},
	} {
		t.Run(tc.layout, func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("ACTIONLINT_SUMMARY", "false")
			switch tc.layout {
			case "outside repository":
				t.Chdir(t.TempDir())
				t.Setenv("ACTIONLINT_CONFIG", "missing-config.yml")
			case "empty repository":
				t.Chdir(t.TempDir())
				gitRepository(t, ".")
			default:
				t.Chdir(commandTestRepo(t))
				if err := os.WriteFile(".github/actionlint.yaml", []byte("tools: {shellcheck: false, ruff: false}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			switch tc.layout {
			case "clean repository":
				if err := os.WriteFile(".github/workflows/ci.yml", []byte(commandGoodWorkflow), 0o600); err != nil {
					t.Fatal(err)
				}
			case "repository subdirectory":
				if err := os.Mkdir("src", 0o700); err != nil {
					t.Fatal(err)
				}
				t.Chdir("src")
			case "file named check":
				if err := os.WriteFile("check", []byte(commandGoodWorkflow), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			cmd := cli.Command{Stdout: &stdout, Stderr: &stderr}
			status := cmd.MainContext(t.Context(), []string{"actionlint"})
			if status != tc.status {
				t.Fatalf("status=%d stdout=%s stderr=%s", status, &stdout, &stderr)
			}
			switch tc.layout {
			case "outside repository":
				if !strings.Contains(stdout.String(), "Usage:") || stderr.Len() != 0 {
					t.Fatalf("outside repository: stdout=%s stderr=%s", &stdout, &stderr)
				}
			case "empty repository":
				if stdout.Len() != 0 || !strings.Contains(stderr.String(), "no workflow or action files") {
					t.Fatalf("empty repository: stdout=%s stderr=%s", &stdout, &stderr)
				}
			case "clean repository":
				if stdout.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("clean repository: stdout=%s stderr=%s", &stdout, &stderr)
				}
			default:
				if !strings.Contains(stdout.String(), `undefined variable "missing"`) || stderr.Len() != 0 {
					t.Fatalf("repository finding: stdout=%s stderr=%s", &stdout, &stderr)
				}
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
