package expressions

import (
	"fmt"
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestScalarScriptPositions(t *testing.T) {
	for _, tc := range []struct {
		script, shell string
		safe          bool
	}{
		{"echo ${{ inputs.count }}", "bash", true},
		{"printf '%s\\n' '${{ inputs.count }}'", "sh", true},
		{"echo \"${{ inputs.count }}\"", "bash", true},
		{"echo ${{ format('}}{0}', inputs.count) }}", "bash", true},
		{"echo 'é' '${{ inputs.count }}'", "bash", true},
		{"${{ inputs.count }} --version", "bash", false},
		{"echo hello; ${{ inputs.count }}", "bash", false},
		{"echo >${{ inputs.count }}", "bash", false},
		{"echo ${{ inputs.count }}>out", "bash", false},
		{"echo ${{ inputs.count }}<in", "sh", false},
		{"echo ${{ inputs.count }}>>out", "bash", false},
		{"echo ${{ inputs.count }}>&1", "sh", false},
		{"echo '${{ inputs.count }}'>out", "bash", true},
		{"echo \"${{ inputs.count }}\">out", "sh", true},
		{"echo ${{ inputs.count }} >out", "bash", true},
		{"echo \"${{ inputs.count }}>out\"", "bash", true},
		{"echo '>${{ inputs.count }}'", "sh", true},
		{"echo x${{ inputs.count }}>out", "bash", true},
		{"echo _${{ inputs.count }}>out", "sh", true},
		{"echo 2${{ inputs.count }}>out", "bash", false},
		{"echo ${{ inputs.count }}${{ inputs.count }}>out", "sh", false},
		{"VALUE=${{ inputs.count }} echo hello", "bash", false},
		{"echo $((${{ inputs.count }}))", "bash", false},
		{"echo $'\\x${{ inputs.count }}'", "bash", false},
		{"echo $\"${{ inputs.count }}\"", "bash", false},
		{"echo ${VALUE${{ inputs.count }}}", "bash", false},
		{"eval '${{ inputs.count }}'", "bash", false},
		{"sh -c '${{ inputs.count }}'", "bash", false},
		{"command ${{ inputs.count }}", "bash", false},
		{"arbitrary --flag '${{ inputs.count }}'", "bash", false},
		{"echo '${{ inputs.count }}", "bash", false},
		{"cat <<EOF\n${{ inputs.count }}\nEOF", "bash", false},
		{"echo '${{ inputs.count }}'", "pwsh", false},
		{"echo '${{ inputs.count }}'", "cmd", false},
		{"echo '${{ inputs.count }}'", "python", false},
		{"echo '${{ inputs.count }}'", "bash {0}", true},
		{"echo '${{ inputs.count }}'", "bash --noprofile --norc -e {0}", true},
		{"echo '${{ inputs.count }}'", "/bin/bash --noprofile --norc -e {0}", true},
		{"echo '${{ inputs.count }}'", "sh -e {0}", true},
		{"echo '${{ inputs.count }}'", "bash -euo pipefail {0}", true},
		{"echo '${{ inputs.count }}'", "bash -- {0}", true},
		{"echo '${{ inputs.count }}'", "bash -c 'python3 {0}'", false},
		{"echo '${{ inputs.count }}'", "bash -ec 'python3 {0}'", false},
		{"echo '${{ inputs.count }}'", "sh -lc 'node {0}'", false},
		{"echo '${{ inputs.count }}'", "bash wrapper.sh {0}", false},
		{"echo '${{ inputs.count }}'", "bash -s {0}", false},
		{"echo '${{ inputs.count }}'", "bash ${{ inputs.options }} {0}", false},
		{"echo '${{ inputs.count }}'", "", false},
	} {
		t.Run(tc.shell+"/"+tc.script, func(t *testing.T) {
			positions := scalarScriptArguments(tc.script, tc.shell)
			if safe := positions[strings.Index(tc.script, "${{")]; safe != tc.safe {
				t.Fatalf("safe=%t, want %t: %v", safe, tc.safe, positions)
			}
		})
	}
}

func TestScalarInterpolationUsesScriptPosition(t *testing.T) {
	for _, expression := range []string{"inputs.count", "inputs.flag", "toJSON(null)", "inputs.flag == true", "toJSON(inputs.flag)"} {
		for _, tc := range []struct {
			name, script, shell, defaults, runner string
			unsafe                                bool
		}{
			{name: "data argument", script: "echo '%s'", runner: "ubuntu-latest"},
			{name: "printf argument", script: "printf '%%s' '%s'", runner: "ubuntu-latest"},
			{name: "command", script: "%s", runner: "ubuntu-latest", unsafe: true},
			{name: "command fragment", script: "tool-%s", runner: "ubuntu-latest", unsafe: true},
			{name: "eval", script: "eval '%s'", runner: "ubuntu-latest", unsafe: true},
			{name: "redirect", script: "echo >%s", runner: "ubuntu-latest", unsafe: true},
			{name: "redirect descriptor", script: "echo %s>out", runner: "ubuntu-latest", unsafe: true},
			{name: "read descriptor", script: "echo %s<in", shell: "sh", runner: "ubuntu-latest", unsafe: true},
			{name: "quoted redirect argument", script: "echo '%s'>out", runner: "ubuntu-latest"},
			{name: "powershell", script: "echo '%s'", runner: "windows-latest", unsafe: true},
			{name: "step shell", script: "echo '%s'", shell: "pwsh", runner: "ubuntu-latest", unsafe: true},
			{name: "workflow shell", script: "echo '%s'", defaults: "defaults: {run: {shell: python}}\n", runner: "ubuntu-latest", unsafe: true},
			{name: "bash overrides Windows", script: "echo '%s'", shell: "bash", runner: "windows-latest"},
			{name: "custom Bash template", script: "echo '%s'", shell: "bash --noprofile --norc -e {0}", runner: "ubuntu-latest"},
			{name: "custom Bash command", script: "%s", shell: "bash --noprofile --norc -e {0}", runner: "ubuntu-latest", unsafe: true},
		} {
			t.Run(expression+"/"+tc.name, func(t *testing.T) {
				script := fmt.Sprintf(tc.script, "${{ "+expression+" }}")
				source := "on: {workflow_dispatch: {inputs: {count: {type: number}, flag: {type: boolean}}}}\n" + tc.defaults + "jobs:\n  test:\n    runs-on: " + tc.runner + "\n    steps:\n      - run: |\n          " + script + "\n"
				if tc.shell != "" {
					source += "        shell: " + tc.shell + "\n"
				}
				workflow, errors := workflowlang.Parse([]byte(source))
				if len(errors) != 0 {
					t.Fatal(errors)
				}
				rule := newTestChecker()
				job := workflow.Jobs["test"]
				for _, err := range []error{rule.VisitWorkflowPre(workflow), rule.VisitJobPre(job), rule.VisitStep(job.Steps[0])} {
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
}

func TestNullInterpolationRetainsTemplateTypeError(t *testing.T) {
	for _, script := range []string{"${{ null }}", "echo '${{ null }}'"} {
		rule := newTestChecker()
		rule.checkScriptString(&workflowlang.String{Value: script, Pos: &workflowlang.Pos{Line: 1, Col: 1}}, "jobs.<job_id>.steps.run", "bash")
		if findings := rule.Findings(); len(findings) != 1 || findings[0].Rule != "expression" || !strings.Contains(findings[0].Message, "type null") {
			t.Fatalf("null must remain rejected by template validation without duplicate interpolation findings: %+v", findings)
		}
	}
}
