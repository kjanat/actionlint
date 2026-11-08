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
		{"default error", "", "", "error", false},
		{"enabled", "lint: {rules: {security: {missing-permissions: on}}}", "", "error", false},
		{"warning", "lint: {rules: {security: {missing-permissions: warn}}}", "", "warning", false},
		{"error", "lint: {rules: {security: {missing-permissions: error}}}", "", "error", false},
		{"disabled", "lint: {rules: {security: {missing-permissions: off}}}", "", "", false},
		{"suppressed", "lint: {rules: {security: {missing-permissions: on}}}", " # ignore[missing-permissions]", "", false},
		{"strict", "lint: {rules: {policy: off}}", "", "error", true},
		{"strict explicit off", "lint: {rules: {policy: off, security: {missing-permissions: off}}}", "", "", true},
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
			if len(findings) != 1 || findings[0].Rule != "missing-permissions" || findings[0].Severity != tc.severity || findings[0].Start.Line != 3 {
				t.Fatalf("expected one %s at job declaration: %+v", tc.severity, findings)
			}
		})
	}
}

func TestMissingPermissionsLocations(t *testing.T) {
	const local = "    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
	for _, tc := range []struct {
		name, option, trigger, workflowPermissions, jobPermissions string
		wantLine                                                   int
	}{
		{"workflow missing", "workflow", "push", "", "    permissions: {}\n", 1},
		{"workflow present", "workflow", "push", "permissions: {}\n", "", 0},
		{"workflow reusable", "workflow", "workflow_call", "", "", 1},
		{"job missing", "every-job", "push", "permissions: {}\n", "", 4},
		{"job present", "every-job", "push", "", "    permissions: {}\n", 0},
		{"job reusable", "every-job", "workflow_call", "", "", 3},
		{"both missing", "[workflow, every-job]", "push", "", "", 3},
		{"both workflow", "[workflow, every-job]", "push", "permissions: {}\n", "", 0},
		{"both job", "[workflow, every-job]", "push", "", "    permissions: {}\n", 0},
		{"both reusable", "[workflow, every-job]", "workflow_call", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: " + tc.trigger + "\n" + tc.workflowPermissions + "jobs:\n  build:\n" + tc.jobPermissions + local
			config := "lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: " + tc.option + "}}}}}"
			findings := testutil.WorkflowDiagnostics(t, source, config)
			if tc.wantLine == 0 {
				if len(findings) != 0 {
					t.Fatalf("unexpected findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Rule != "missing-permissions" || findings[0].Severity != "error" || findings[0].Start.Line != tc.wantLine {
				t.Fatalf("expected permission error at line %d: %+v", tc.wantLine, findings)
			}
		})
	}
}
