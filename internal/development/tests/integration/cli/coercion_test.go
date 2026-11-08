package cli_test

import (
	"os"
	"strings"
	"testing"
)

func TestCommandSuspiciousCoercionSelection(t *testing.T) {
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - if: contains(null, '') && startsWith(true, 'TR')
        run: echo ok
permissions: {}
`
	for _, tc := range []struct {
		name, config, severity string
	}{
		{"default", "lint: {rules: {preset: none}}", ""},
		{"off", "lint: {rules: {preset: none, suspicious: {coercion: off}}}", ""},
		{"on", "lint: {rules: {preset: none, suspicious: {coercion: on}}}", "warning"},
		{"legacy on", "lint: {rules: {preset: none, suspicious: {suspicious-coercion: on}}}", "warning"},
		{"error", "lint: {rules: {preset: none, suspicious: {coercion: error}}}", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commandTestRepo(t)
			if err := os.WriteFile(".github/workflows/ci.yml", []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(".github/actionlint.yaml", []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, prefix := range [][]string{nil, {"check"}} {
				got := testRunCommand("", append(append([]string{}, prefix...), "--config=.github/actionlint.yaml", "--output=pretty", ".github/workflows/ci.yml")...)
				if tc.severity == "" {
					if got != (commandTranscript{}) {
						t.Fatalf("%v: disabled coercion warning produced output: %+v", prefix, got)
					}
					continue
				}
				if got.Status != 1 || got.Stderr != "" || strings.Count(got.Stdout, tc.severity+"[coercion]\n") != 2 || strings.Contains(got.Stdout, "[expression-type]") {
					t.Fatalf("%v: expected two %s coercion diagnostics and exit 1: %+v", prefix, tc.severity, got)
				}
			}
		})
	}
}
