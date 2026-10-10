package actionlint

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRuleRequirePermissions(t *testing.T) {
	const steps = "    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"
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
		{"call explicit", "{scope: job}", "", "    permissions: {}\n    uses: example/repo/.github/workflows/ci.yml@main\n", 0},
	} {
		for _, assumption := range []string{"restricted", "permissive"} {
			t.Run(tc.name+"/"+assumption, func(t *testing.T) {
				cfg, err := ParseConfig(fmt.Appendf(nil, "assume-default-permissions: %s\npolicy: {require-permissions: %s}", assumption, tc.policy))
				if err != nil {
					t.Fatal(err)
				}
				linter, err := NewLinter(io.Discard, &LinterOptions{Shellcheck: "", Pyflakes: ""})
				if err != nil {
					t.Fatal(err)
				}
				linter.defaultConfig = cfg
				source := []byte("on: push\n" + tc.workflowPermissions + "jobs:\n  test:\n" + tc.job)
				errs, err := linter.Lint("test.yml", source, nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantLine == 0 {
					if len(errs) != 0 {
						t.Fatalf("unexpected errors: %v", errs)
					}
					return
				}
				if len(errs) != 1 || errs[0].Kind != "require-permissions" || errs[0].Line != tc.wantLine || !strings.Contains(errs[0].Message, "permissions: {}") {
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
				"name: # actionlint:ignore require-permissions -- reviewed\n  !!str\n  |\n    CI\non: push",
			} {
				source := strings.ReplaceAll(prefix+declaration+"\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", "\n", ending)
				if got := lintCachePolicy(t, source, "policy: {require-permissions: true}"); len(got) != 0 {
					t.Fatalf("workflow permission finding cannot be suppressed in %q: %+v", source, got)
				}
			}
		}
	}
}

func TestWorkflowPermissionsDeclarationPosition(t *testing.T) {
	policy, err := RequirePermissions("workflow")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prefix string
		line   int
	}{
		{"", 1},
		{"# license\n\n", 3},
		{"# license\n---\n", 3},
	} {
		for _, declaration := range []string{"on: push", "name:\n  !!str\n  |\n    CI\non: push", "name: &title CI\nrun-name: *title\non: push"} {
			for _, ending := range []string{"\n", "\r\n"} {
				source := strings.ReplaceAll(tc.prefix+declaration+"\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", "\n", ending)
				workflow, findings := Parse([]byte(source))
				if len(findings) != 0 || workflow == nil || workflow.Pos == nil || *workflow.Pos != (Pos{Line: tc.line, Col: 1}) {
					t.Fatalf("missing workflow declaration position: %+v, %+v", workflow, findings)
				}
				rule := NewRuleRequirePermissions(policy)
				if err := rule.VisitWorkflowPre(workflow); err != nil {
					t.Fatal(err)
				}
				if findings := rule.Errs(); len(findings) != 1 || findings[0].Line != tc.line || findings[0].Column != 1 {
					t.Fatalf("permission finding lost declaration position: %+v", findings)
				}
			}
		}
	}
	rule := NewRuleRequirePermissions(policy)
	if err := rule.VisitWorkflowPre(&Workflow{}); err != nil {
		t.Fatal(err)
	}
	if findings := rule.Errs(); len(findings) != 1 || findings[0].Line != 1 || findings[0].Column != 1 {
		t.Fatalf("manual AST fallback changed: %+v", findings)
	}
}

func TestWorkflowPermissionsSuppressionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		header, declaration, policy string
		want                        []string
	}{
		{"# actionlint:ignore require-permissions -- detached\n# license\n", "on: push", "require-permissions: true", []string{"require-permissions"}},
		{"# license\n", "on: push # actionlint:ignore require-permissions", "require-permissions: true", []string{"require-permissions", "inline-suppression"}},
		{"# license\n", "on: push # actionlint:ignore require-permissions -- reviewed", "require-permissions: true, disallow-suppressions: true", []string{"require-permissions", "disallow-suppressions"}},
		{"# license\n", "on: push # actionlint:ignore require-permissions -- workflow only", "require-permissions: {scope: job}", []string{"require-permissions"}},
	} {
		source := tc.header + tc.declaration + "\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
		findings := lintCachePolicy(t, source, "policy: {"+tc.policy+"}")
		if len(findings) != len(tc.want) {
			t.Fatalf("permission suppression scope changed: %+v", findings)
		}
		for i, kind := range tc.want {
			if findings[i].Kind != kind {
				t.Fatalf("want %v, got %+v", tc.want, findings)
			}
		}
	}
}
