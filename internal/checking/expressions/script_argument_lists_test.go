package expressions

import (
	"fmt"
	"strings"
	"testing"
)

func TestClosedScriptArgumentLists(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script, env, shell string
		unsafe                   bool
	}{
		{name: "literal argument list", script: "tool ${{ env.ARGS }}", env: "--flag value"},
		{name: "finite template", script: "tool ${{ env.ARGS }}", env: "--diff ${{ case(github.event_name == 'push', '--exclude .github', '') }}"},
		{name: "direct finite expression", script: "tool ${{ case(github.event_name == 'push', '--exclude .github', '') }}"},
		{name: "single quoted list", script: "tool '${{ env.ARGS }}'", env: "--flag value"},
		{name: "double quoted list", script: `tool "${{ env.ARGS }}"`, env: "--flag value"},
		{name: "tabs", script: "tool ${{ env.ARGS }}", env: "--flag\tvalue"},
		{name: "unknown output", script: "tool ${{ env.ARGS }}", env: "${{ case(github.event_name == 'push', '--safe', github.ref) }}", unsafe: true},
		{name: "operator", script: "tool ${{ env.ARGS }}", env: "--flag; id", unsafe: true},
		{name: "substitution", script: "tool ${{ env.ARGS }}", env: "$(id)", unsafe: true},
		{name: "quoted operator", script: "tool '${{ env.ARGS }}'", env: "--flag'; id; #", unsafe: true},
		{name: "quoted expansion", script: `tool "${{ env.ARGS }}"`, env: "$(id)", unsafe: true},
		{name: "command position", script: "${{ env.ARGS }} --flag", env: "tool --evil", unsafe: true},
		{name: "dynamic command", script: "${{ inputs.tool }} ${{ env.ARGS }}", env: "--flag value", unsafe: true},
		{name: "assignment", script: "VALUE=${{ env.ARGS }} tool", env: "--flag value", unsafe: true},
		{name: "redirect", script: "tool >${{ env.ARGS }}", env: "--flag value", unsafe: true},
		{name: "IO descriptor", script: "tool ${{ env.ARGS }}>out", env: "2", unsafe: true},
		{name: "quoted before redirect", script: "tool '${{ env.ARGS }}'>out", env: "--flag value"},
		{name: "word concatenation", script: "tool prefix${{ env.ARGS }}", env: "--flag value", unsafe: true},
		{name: "heredoc", script: "cat <<EOF\n${{ env.ARGS }}\nEOF", env: "--flag value", unsafe: true},
		{name: "unsupported shell", script: "tool ${{ env.ARGS }}", env: "--flag value", shell: "pwsh", unsafe: true},
		{name: "evaluated expression delimiters", script: "tool ${{ env.ARGS }}", env: "${{ '${{ inputs.tool }}' }}", unsafe: true},
		{name: "unknown mapping", script: "tool ${{ env.ARGS }}", env: "--flag value", unsafe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			shell := tc.shell
			if shell == "" {
				shell = "bash"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + shell + "\n        env:\n          ARGS: " + fmt.Sprintf("%q", tc.env) + "\n"
			if tc.name == "unknown mapping" {
				source += "          ${{ insert }}: ${{ fromJSON(vars.EXTRA_ENV) }}\n"
			}
			source += "        run: |\n          " + strings.ReplaceAll(tc.script, "\n", "\n          ") + "\n"
			rule, workflow := newTestCheckerForSource(t, source)
			job := testJob(t, workflow, "test")
			for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(testStep(t, job, 0))} {
				if err != nil {
					t.Fatal(err)
				}
			}
			unsafe := false
			for _, finding := range rule.Findings() {
				unsafe = unsafe || finding.Rule == "script-interpolation"
			}
			if unsafe != tc.unsafe {
				t.Fatalf("unsafe=%t, want %t: %+v", unsafe, tc.unsafe, rule.Findings())
			}
		})
	}
}
