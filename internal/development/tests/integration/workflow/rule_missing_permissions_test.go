package workflow_test

import (
	"fmt"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestMissingPermissionsSelectionAndSuppression(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
	const local = "    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
	for _, tc := range []struct {
		name, option, trigger, workflowPermissions, jobPermissions string
		wantLines                                                  []int
	}{
		{"default workflow", "", "push", "permissions: {}\n", "", nil},
		{"default job", "", "push", "", "    permissions: {}\n", nil},
		{"default missing", "", "push", "", "", []int{3}},
		{"default reusable", "", "workflow_call", "", "", nil},
		{"workflow missing", "workflow", "push", "", "    permissions: {}\n", []int{1}},
		{"workflow present", "workflow", "push", "permissions: {}\n", "", nil},
		{"workflow reusable", "workflow", "workflow_call", "", "", []int{1}},
		{"job missing", "every-job", "push", "permissions: {}\n", "", []int{4}},
		{"job present", "every-job", "push", "", "    permissions: {}\n", nil},
		{"job reusable", "every-job", "workflow_call", "", "", []int{3}},
		{"both missing", "[workflow, every-job]", "push", "", "", []int{1, 3}},
		{"both workflow only", "[workflow, every-job]", "push", "permissions: {}\n", "", []int{4}},
		{"both job only", "[workflow, every-job]", "push", "", "    permissions: {}\n", []int{1}},
		{"both present", "[workflow, every-job]", "push", "permissions: {}\n", "    permissions: {}\n", nil},
		{"both reversed", "[every-job, workflow]", "push", "permissions: {}\n", "", []int{4}},
		{"both reusable", "[workflow, every-job]", "workflow_call", "", "", []int{1, 3}},
		{"workflow array", "[workflow]", "push", "", "    permissions: {}\n", []int{1}},
		{"job array", "[every-job]", "push", "permissions: {}\n", "", []int{4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "on: " + tc.trigger + "\n" + tc.workflowPermissions + "jobs:\n  build:\n" + tc.jobPermissions + local
			config := "lint: {rules: {security: {missing-permissions: {level: error, options: {require-at: " + tc.option + "}}}}}"
			if tc.option == "" {
				config = "lint: {rules: {security: {missing-permissions: error}}}"
			}
			findings := testutil.WorkflowDiagnostics(t, source, config)
			var lines []int
			for _, finding := range findings {
				if finding.Rule != "missing-permissions" || finding.Severity != "error" {
					t.Fatalf("unexpected finding: %+v", finding)
				}
				lines = append(lines, finding.Start.Line)
			}
			if !slices.Equal(lines, tc.wantLines) {
				t.Fatalf("permission errors at lines %v, want %v: %+v", lines, tc.wantLines, findings)
			}
		})
	}
}
