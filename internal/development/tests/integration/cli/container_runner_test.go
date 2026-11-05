package cli_test

import (
	"os"
	"strings"
	"testing"
)

func TestCommandContainerRunner(t *testing.T) {
	for _, tc := range []struct {
		level, severity string
		status          int
	}{
		{"on", "error", 1},
		{"warn", "warning", 1},
		{"off", "", 0},
	} {
		t.Run(tc.level, func(t *testing.T) {
			commandTestRepo(t)
			if err := os.WriteFile(".github/workflows/ci.yml", []byte("on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: docker://alpine:3\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			config := "tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, correctness: {container-runner: " + tc.level + "}}}\n"
			if err := os.WriteFile(".github/actionlint.yaml", []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			got := testRunCommand("", "--output=pretty", ".github/workflows/ci.yml")
			if got.Status != tc.status || got.Stderr != "" {
				t.Fatalf("unexpected exit: %+v", got)
			}
			if tc.severity == "" {
				if got.Stdout != "" {
					t.Fatalf("disabled rule emitted output: %+v", got)
				}
			} else if !strings.Contains(got.Stdout, tc.severity+"[container-runner]") {
				t.Fatalf("missing rendered diagnostic: %+v", got)
			}
		})
	}
}
