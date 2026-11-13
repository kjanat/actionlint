package security_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/security"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestMissingPermissions(t *testing.T) {
	t.Parallel()
	if security.MissingPermissionsDeclaration().DefaultLevel != "error" || !security.MissingPermissionsDeclaration().Recommended {
		t.Fatal("missing permissions must be an error by default")
	}
	const local = "    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
	const call = "    uses: example/repo/.github/workflows/build.yml@main\n"
	for _, tc := range []struct {
		name, trigger, permissions, jobs string
		lines                            []int
	}{
		{"missing", "on: push\n", "", "  build:\n" + local, []int{3}},
		{"multiple events report once", "on: [push, pull_request]\n", "", "  build:\n" + local, []int{3}},
		{"workflow empty", "on: push\n", "permissions: {}\n", "  build:\n" + local, nil},
		{"workflow read", "on: push\n", "permissions: read-all\n", "  build:\n" + local, nil},
		{"workflow write", "on: push\n", "permissions: write-all\n", "  build:\n" + local, nil},
		{"workflow scoped", "on: push\n", "permissions: {contents: read}\n", "  build:\n" + local, nil},
		{"job empty", "on: push\n", "", "  build:\n    permissions: {}\n" + local, nil},
		{"job read", "on: push\n", "", "  build:\n    permissions: read-all\n" + local, nil},
		{"job write", "on: push\n", "", "  build:\n    permissions: write-all\n" + local, nil},
		{"job scoped", "on: push\n", "", "  build:\n    permissions: {contents: read}\n" + local, nil},
		{"only uncovered job", "on: push\n", "", "  covered:\n    permissions: {}\n" + local + "  uncovered:\n" + local, []int{7}},
		{"both jobs uncovered", "on: push\n", "", "  first:\n" + local + "  second:\n" + local, []int{3, 6}},
		{"reusable callee", "on: workflow_call\n", "", "  build:\n" + local, nil},
		{"reusable mapping", "on: {workflow_call: {}}\n", "", "  build:\n" + local, nil},
		{"mixed callee", "on: [workflow_call, push]\n", "", "  build:\n" + local, []int{3}},
		{"mixed reversed", "on: [push, workflow_call]\n", "", "  build:\n" + local, []int{3}},
		{"manual", "on: workflow_dispatch\n", "", "  build:\n" + local, []int{3}},
		{"schedule", "on: {schedule: [{cron: '0 0 * * *'}]}\n", "", "  build:\n" + local, []int{3}},
		{"reusable caller", "on: push\n", "", "  build:\n" + call, []int{3}},
		{"covered caller", "on: push\n", "", "  build:\n    permissions: {}\n" + call, nil},
		{"no triggers", "on: {}\n", "", "  build:\n" + local, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := tc.trigger + tc.permissions + "jobs:\n" + tc.jobs
			ast, errs := workflowparser.Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			view := declaration.Publish(source, ast, false, "").Bind("test").Workflow()
			var lines []int
			for _, finding := range security.MissingPermissionsCheck(nil).Workflow(view) {
				if finding.Rule != "missing-permissions" || finding.Severity != "" || finding.Start.Column != 3 {
					t.Fatalf("unexpected diagnostic: %+v", finding)
				}
				lines = append(lines, finding.Start.Line)
			}
			slices.Sort(lines)
			if !slices.Equal(lines, tc.lines) {
				t.Fatalf("uncovered jobs at lines %v, want %v", lines, tc.lines)
			}
		})
	}
}
