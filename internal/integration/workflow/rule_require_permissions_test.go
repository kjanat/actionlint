package workflow_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestRuleRequirePermissions(t *testing.T) {
	const steps = `    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`
	for _, tc := range []struct {
		name, policy, workflowPermissions, job string
		wantLine                               int
	}{
		{"unset", "null", "", steps, 0},
		{"disabled", "false", "", steps, 0},
		{"workflow missing", "true", "", steps, 1},
		{"workflow empty", "true", "permissions: {}\n", steps, 0},
		{"workflow read all", "true", "permissions: read-all\n", steps, 0},
		{"workflow write all", "true", "permissions: write-all\n", steps, 0},
		{"workflow scoped", "true", "permissions: {contents: read}\n", steps, 0},
		{"job does not replace workflow declaration", "true", "", "    permissions: {}\n" + steps, 1},
		{"job missing", "{scope: job}", "", steps, 3},
		{"job does not inherit declaration", "{scope: job}", "permissions: {}\n", steps, 4},
		{"job empty", "{scope: job}", "", "    permissions: {}\n" + steps, 0},
		{"job scoped", "{scope: job}", "", "    permissions: {contents: read}\n" + steps, 0},
		{"later job missing", "{scope: job}", "", "    permissions: {}\n" + steps + "  other:\n" + steps, 8},
		{"call missing", "{scope: job}", "", "    uses: example/repo/.github/workflows/ci.yml@main\n", 3},
		{"call explicit", "{scope: job}", "", `    permissions: {}
    uses: example/repo/.github/workflows/ci.yml@main
`, 0},
	} {
		for _, assumption := range []string{"restricted", "permissive"} {
			t.Run(tc.name+"/"+assumption, func(t *testing.T) {
				cfg, err := repoconfig.Parse(fmt.Appendf(nil, "assume-default-permissions: %s\npolicy: {require-permissions: %s}", assumption, tc.policy))
				if err != nil {
					t.Fatal(err)
				}
				request := application.Options{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
				var requestSources []workspace.Source
				linterConfig := cfg
				source := []byte("on: push\n" + tc.workflowPermissions + `jobs:
  test:
` + tc.job)
				requestSources = []workspace.Source{{Path: "test.yml", InputPath: "test.yml", Content: source, Project: nil, Config: linterConfig}}
				errs, err := testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantLine == 0 {
					if len(errs) != 0 {
						t.Fatalf("unexpected errors: %v", errs)
					}
					return
				}
				if len(errs) != 1 || errs[0].Rule != "require-permissions" || errs[0].Start.Line != tc.wantLine || !strings.Contains(errs[0].Message, "permissions: {}") {
					t.Fatalf("expected one permissions error at line %d, got %v", tc.wantLine, errs)
				}
			})
		}
	}
}

func TestWorkflowPermissionsSuppressionLocation(t *testing.T) {
	for _, prefix := range []string{"# license\n", "\n\n", "# license\n\n---\n"} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, declaration := range []string{
				"on: push # actionlint:ignore require-permissions -- reviewed",
				"# actionlint:ignore-next-line require-permissions -- reviewed\non: push",
				`name: # actionlint:ignore require-permissions -- reviewed
  !!str
  |
    CI
on: push`,
			} {
				source := strings.ReplaceAll(prefix+declaration+`
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`, "\n", ending)
				if got := testutil.WorkflowDiagnostics(t, source, "policy: {require-permissions: true}"); len(got) != 0 {
					t.Fatalf("workflow permission finding cannot be suppressed in %q: %+v", source, got)
				}
			}
		}
	}
}

func TestWorkflowPermissionsSuppressionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		header, declaration, policy string
		want                        []string
	}{
		{"# actionlint:ignore require-permissions -- detached\n# license\n", "on: push", "require-permissions: true", []string{"require-permissions"}},
		{"# license\n", "on: push # actionlint:ignore require-permissions", "require-permissions: true", nil},
		{"# license\n", "on: push # actionlint:ignore require-permissions -- reviewed", "require-permissions: true, disallow-suppressions: true", []string{"require-permissions", "disallow-suppressions"}},
		{"# license\n", "on: push # actionlint:ignore require-permissions -- workflow only", "require-permissions: {scope: job}", []string{"require-permissions"}},
	} {
		source := tc.header + tc.declaration + `
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`
		findings := testutil.WorkflowDiagnostics(t, source, "policy: {"+tc.policy+"}")
		if len(findings) != len(tc.want) {
			t.Fatalf("permission suppression scope changed: %+v", findings)
		}
		for i, kind := range tc.want {
			if findings[i].Rule != kind {
				t.Fatalf("want %v, got %+v", tc.want, findings)
			}
		}
	}
}
