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
