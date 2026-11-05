package workflow_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestMissingPermissionsSelectionAndSuppression(t *testing.T) {
	const workflow = "on: push\njobs:\n  build:%s\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
	for _, tc := range []struct {
		name, config, comment, severity string
		strict                          bool
	}{
		{"default off", "", "", "", false},
		{"enabled", "lint: {rules: {security: {missing-workflow-permissions: on}}}", "", "warning", false},
		{"error", "lint: {rules: {security: {missing-workflow-permissions: error}}}", "", "error", false},
		{"disabled", "lint: {rules: {security: {missing-workflow-permissions: off}}}", "", "", false},
		{"suppressed", "lint: {rules: {security: {missing-workflow-permissions: on}}}", " # ignore[missing-workflow-permissions]", "", false},
		{"strict", "lint: {rules: {policy: off}}", "", "error", true},
		{"strict explicit off", "lint: {rules: {policy: off, security: {missing-workflow-permissions: off}}}", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf(workflow, tc.comment)
			findings := testutil.WorkflowDiagnosticsWithOptions(t, source, tc.config, checking.Options{Presets: repoconfig.Presets{Strict: tc.strict}})
			if tc.severity == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Rule != "missing-workflow-permissions" || findings[0].Severity != tc.severity || findings[0].Start.Line != 3 {
				t.Fatalf("expected one %s at job declaration: %+v", tc.severity, findings)
			}
		})
	}
}
