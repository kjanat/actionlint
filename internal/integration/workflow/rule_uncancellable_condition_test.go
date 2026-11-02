package workflow_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestUncancellableConditionSelectionAndSuppression(t *testing.T) {
	const workflow = "on: push\npermissions: {}\njobs:\n  build:\n    if: ${{ always() }}%s\n    timeout-minutes: 5\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
	for _, tc := range []struct {
		name, config, comment, severity string
		strict                          bool
	}{
		{"default", "", "", "warning", false},
		{"strict", "lint: {rules: {policy: off}}", "", "error", true},
		{"disabled", "lint: {rules: {correctness: {uncancellable-condition: off}}}", "", "", false},
		{"strict disabled", "lint: {rules: {policy: off, correctness: {uncancellable-condition: off}}}", "", "", true},
		{"configured info", "lint: {rules: {correctness: {uncancellable-condition: info}}}", "", "info", false},
		{"suppressed", "", " # ignore[uncancellable-condition]", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings := testutil.WorkflowDiagnosticsWithOptions(t, fmt.Sprintf(workflow, tc.comment), tc.config, application.Options{Presets: repoconfig.Presets{Strict: tc.strict}})
			if tc.severity == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Rule != "uncancellable-condition" || findings[0].Severity != tc.severity || findings[0].Start.Line != 5 {
				t.Fatalf("unexpected findings: %+v", findings)
			}
			if len(findings[0].Fixes) != 1 || findings[0].Fixes[0].Edits[0].Path != "test.yaml" {
				t.Fatalf("fix was not admitted: %+v", findings[0])
			}
		})
	}
}

func TestBoundedCheckoutCancellationSeverity(t *testing.T) {
	const workflow = "on: push\npermissions: {}\njobs:\n  build:\n    cancel-timeout-minutes: 1\n    timeout-minutes: 5\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n        if: always()\n"
	for _, tc := range []struct {
		name, config, severity string
		strict                 bool
	}{
		{"default", "", "info", false},
		{"explicit warning", "lint: {rules: {correctness: {uncancellable-condition: warn}}}", "warning", false},
		{"strict", "lint: {rules: {policy: off}}", "error", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings := testutil.WorkflowDiagnosticsWithOptions(t, workflow, tc.config, application.Options{Presets: repoconfig.Presets{Strict: tc.strict}})
			found := false
			for _, finding := range findings {
				if finding.Rule == "uncancellable-condition" {
					found = true
					if finding.Severity != tc.severity {
						t.Fatalf("want %s: %+v", tc.severity, finding)
					}
				}
			}
			if !found {
				t.Fatalf("missing diagnostic: %+v", findings)
			}
		})
	}
}
