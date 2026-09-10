package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestCommandWorkflowPaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	for _, dir := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, ".github", "workflows")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(root, "outside.yml")
	for _, path := range []string{outside, filepath.Join(repo, ".github", "workflows", "ci.yml")} {
		if err := os.WriteFile(path, []byte(commandBadWorkflow), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(repo)
	for _, modern := range []bool{false, true} {
		for _, input := range []string{"", outside, filepath.Join("..", "outside.yml")} {
			t.Run(fmtTestName(modern, input), func(t *testing.T) {
				args := []string{"--json"}
				if input != "" {
					args = append(args, input)
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand("", args...)
				var report actionlint.CheckResult
				if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil || got.Status != 1 || got.Stderr != "" || len(report.Diagnostics) != 1 {
					t.Fatalf("selected workflow was not analyzed: %+v (%v)", got, err)
				}
				want := filepath.Join("..", "outside.yml")
				if input == "" {
					want = filepath.Join(".github", "workflows", "ci.yml")
				}
				if report.Diagnostics[0].Path != want || report.Diagnostics[0].Rule != "expression" {
					t.Fatalf("wrong workflow diagnostic: %+v", report.Diagnostics[0])
				}
			})
		}
	}
}

func TestLegacyCLITextWriteErrors(t *testing.T) {
	for _, format := range []actionlint.OutputFormat{actionlint.OutputFormatText, actionlint.OutputFormatOneline, actionlint.OutputFormatJSON, actionlint.OutputFormatJSONL, actionlint.OutputFormatSARIF, actionlint.OutputFormatGitHub} {
		text := format == actionlint.OutputFormatText || format == actionlint.OutputFormatOneline
		var stderr strings.Builder
		cmd := Command{Stdin: strings.NewReader(commandBadWorkflow), Stdout: commandFailingIO{}, Stderr: &stderr}
		status := cmd.Main([]string{"actionlint", "--shellcheck=", "--pyflakes=", "--output-format=" + string(format), "-"})
		if text && (status != 1 || stderr.Len() != 0) || !text && (status != 3 || !strings.Contains(stderr.String(), "write failed")) {
			t.Fatalf("%s changed CLI streams or status: %d, %s", format, status, &stderr)
		}
	}
}
