package expressions

import (
	"strings"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	runner "actionlint.kjanat.dev/v2/internal/model/runners"
)

func TestScriptEnvironmentReplacements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		script, shell, variable string
	}{
		{`bun "${{ github.action_path }}/../runtime/client.mjs" begin`, "bash", "GITHUB_ACTION_PATH"},
		{`bun "${{ github['action_path'] }}/client.mjs" begin`, "sh", "GITHUB_ACTION_PATH"},
		{`cat "${{ github.workspace }}/file"`, "bash --noprofile --norc -e {0}", "GITHUB_WORKSPACE"},
		{`cat "${{ runner.temp }}/file"`, "bash", "RUNNER_TEMP"},
		{`echo "${{ github.ref_name }}suffix"`, "bash", "GITHUB_REF_NAME"},
		{`echo "${{ github.event.issue.title }}"`, "bash", ""},
		{`echo "${{ github.token }}"`, "bash", ""},
		{`echo "${{ inputs.path }}"`, "bash", ""},
		{`echo "${{ env.PATH }}"`, "bash", ""},
		{`echo "${{ github.action_path || github.workspace }}"`, "bash", ""},
		{`echo "${{ github.ref_protected }}"`, "bash", ""},
		{`echo '${{ github.action_path }}'`, "bash", ""},
		{`echo ${{ github.action_path }}`, "bash", ""},
		{`echo $"${{ github.action_path }}"`, "bash", ""},
		{`echo "\${{ github.action_path }}"`, "bash", ""},
		{`echo "\\${{ github.action_path }}"`, "bash", "GITHUB_ACTION_PATH"},
		{`eval "echo ${{ github.action_path }}"`, "bash", ""},
		{`bash -c "echo ${{ github.action_path }}"`, "bash", ""},
		{`bash -lc "echo ${{ github.action_path }}"`, "bash", ""},
		{`bun --eval "console.log('${{ github.action_path }}')"`, "bash", ""},
		{`echo "${{ github.action_path }}"`, "pwsh", ""},
		{`echo "${{ github.action_path }}"`, "python", ""},
		{`echo "${{ github.action_path }}"`, "bash -c 'python {0}'", ""},
		{`echo "${{ github.action_path }}`, "bash", ""},
		{"cat <<EOF\n${{ github.action_path }}\nEOF", "bash", ""},
		{`# echo "${{ github.action_path }}"`, "bash", ""},
	} {
		t.Run(tc.shell+"/"+tc.script, func(t *testing.T) {
			t.Parallel()
			proofs := proveScriptArguments(tc.script, tc.shell)
			first := strings.Index(tc.script, "${{")
			if replacement := proofs.environment[first]; replacement.environment.Name() != tc.variable {
				t.Fatalf("got %+v, want variable %q", replacement, tc.variable)
			}
		})
	}
}

func TestScriptEnvironmentFixIsDetached(t *testing.T) {
	t.Parallel()
	rule, workflow := newTestCheckerForSource(t, "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ github.workspace }}\"\n        shell: bash\n")
	job := testJob(t, workflow, "test")
	for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(testStep(t, job, 0))} {
		if err != nil {
			t.Fatal(err)
		}
	}
	findings := rule.Findings()
	if len(findings) != 1 || len(findings[0].Fixes) != 1 || len(findings[0].Fixes[0].Edits) != 1 {
		t.Fatalf("missing fix: %+v", findings)
	}
	findings[0].Fixes[0].Edits[0].Replacement = "corrupted"
	findings[0].Fixes[0].Description = "corrupted"
	if fresh := rule.Findings(); fresh[0].Fixes[0].Edits[0].Replacement != "${GITHUB_WORKSPACE}" || fresh[0].Fixes[0].Description == "corrupted" {
		t.Fatalf("a consumer changed the stored fix: %+v", fresh)
	}
}

func TestScriptEnvironmentVariable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ expression, variable string }{
		{"github.action_path", "GITHUB_ACTION_PATH"},
		{"GitHub['WORKSPACE']", "GITHUB_WORKSPACE"},
		{"runner.temp", "RUNNER_TEMP"},
		{"github.event.issue.title", ""},
		{"github.token", ""},
		{"github.action_status", ""},
		{"runner.workspace", ""},
		{"format('{0}', github.workspace)", ""},
	} {
		expression, err := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.expression + " }}"))
		if err != nil {
			t.Fatal(err)
		}
		if got := runner.ContextEnvironmentFor(exprsyntax.StaticReference(expression)).Name(); got != tc.variable {
			t.Errorf("%s: got %q, want %q", tc.expression, got, tc.variable)
		}
	}
}
