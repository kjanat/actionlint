package config_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestRuleSettingsAnalysis(t *testing.T) {
	const source = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    if: true
    steps:
      - run: echo ok
`
	for _, level := range []string{"off", "on", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			findings := testutil.WorkflowDiagnostics(t, source, "lint: {rules: {correctness: {if-cond: "+level+"}}}")
			if level == "off" {
				if len(findings) != 0 {
					t.Fatal(findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Rule != "if-cond" {
				t.Fatal(findings)
			}
			want := level
			if level == "on" {
				want = ""
			}
			if level == "warn" {
				want = "warning"
			}
			if findings[0].Severity != want {
				t.Fatalf("got %q, want %q", findings[0].Severity, want)
			}
		})
	}
	if got := testutil.WorkflowDiagnostics(t, source, "lint: {enabled: false}"); len(got) != 0 {
		t.Fatal(got)
	}
	findings := testutil.WorkflowDiagnostics(t, source, "lint: {rules: {correctness: {if-cond: off}, policy: {require-job-timeout: {level: warn, options: {max-minutes: 5}}}}}")
	if len(findings) != 1 || findings[0].Rule != "require-job-timeout" || findings[0].Severity != "warning" {
		t.Fatal(findings)
	}
}
