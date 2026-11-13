package actions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
)

func TestInvokedCompositeExpressionSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ expression, rule, code string }{
		{"fromJSON(steps.check.outputs).available", "expression-call", "invalid-json"},
		{"steps.check.outputs.available", "string-conditions", "string-conditions"},
		{"join('[github.server_url,github.repository]', '/')", "join-literal", "join-literal"},
		{"fromJSON(steps.check.outputs.available)", "", ""},
		{"fromJSON(true)", "", ""},
		{"fromJSON(42)", "", ""},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nlint:\n  rules:\n    preset: none\n    correctness:\n      expression-call: error\n      expression-type: error\n    suspicious:\n      string-conditions: warn\n      join-literal: info\n")
			const prefix = "name: inner\ndescription: Check expressions from an invocation.\nruns:\n  using: composite\n  steps:\n    - id: check\n      shell: bash\n      run: echo available=false >> \"$GITHUB_OUTPUT\"\n    - shell: bash\n      run: echo checked\n      if: ${{ "
			fixturefiles.Write(t, root, "inner/action.yml", prefix+tc.expression+" }}\n")
			result := testutil.Composite(t, root, "- uses: ./inner", checking.Options{})
			want := 0
			if tc.rule != "" {
				want = 1
			}
			if len(result.Diagnostics) != want {
				t.Fatalf("findings=%+v, want %d", result.Diagnostics, want)
			}
			if want == 0 {
				return
			}
			finding := result.Diagnostics[0]
			if finding.Rule != tc.rule || finding.Code != tc.code || !strings.HasSuffix(strings.ReplaceAll(finding.Path, "\\", "/"), "inner/action.yml") || finding.Start.Line != 11 {
				t.Fatalf("lost composite diagnostic identity or source: %+v", finding)
			}
		})
	}
}
