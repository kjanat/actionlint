package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSuspiciousCoercionSelection(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			dir := commandTestRepo(t)
			path := filepath.Join(dir, ".github", "workflows", "ci.yml")
			if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(dir, ".github", "actionlint.yaml")
			if err := os.WriteFile(config, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, prefix := range [][]string{nil, {"check"}} {
				got := testRunCommand("", append(append([]string{}, prefix...), "--config="+config, "--output=pretty", path)...)
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
