package execution_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

func TestEnvironmentInsertionInvocationSafety(t *testing.T) {
	for _, tc := range []struct {
		name, value           string
		checkout, shell, path bool
	}{
		{"unknown", "${{ fromJSON(vars.ENV) }}", true, true, true},
		{"safe literals", "{VALUE: text}", false, false, false},
		{"constant object", `${{ fromJSON('{"VALUE":"text"}') }}`, false, false, false},
		{"empty object", `${{ fromJSON('{}') }}`, false, false, false},
		{"constant path", `${{ fromJSON('{"PATH":"custom"}') }}`, true, false, true},
		{"constant startup", `${{ fromJSON('{"BASH_ENV":"startup.sh"}') }}`, false, true, false},
		{"constant empty startup", `${{ fromJSON('{"BASH_ENV":""}') }}`, false, false, false},
		{"constant loader", `${{ fromJSON('{"LD_PRELOAD":"custom.so"}') }}`, true, true, false},
		{"constant empty loader", `${{ fromJSON('{"LD_PRELOAD":""}') }}`, false, false, false},
		{"constant checkout wrapper", `${{ fromJSON('{"GIT_EXEC_PATH":"custom"}') }}`, true, false, false},
		{"evaluated value remains data", `${{ fromJSON('{"BASH_ENV":"${{ '''' }}"}') }}`, false, true, false},
		{"evaluated name remains data", `${{ fromJSON('{"${{ ''PATH'' }}":"custom"}') }}`, false, false, false},
		{"invalid object", `${{ fromJSON('[]') }}`, true, true, true},
		{"nested path", "{'${{ insert }}': {PATH: custom}}", true, false, true},
		{"shell startup", "{BASH_ENV: startup.sh}", false, true, false},
		{"checkout wrapper", "{GIT_EXEC_PATH: custom}", true, false, false},
		{"loader", "{LD_PRELOAD: custom.so}", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := workflowlang.ParseDocument([]byte("on: push\nenv:\n  ${{ insert }}: " + tc.value + "\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			env := workflow.Env
			if len(env.Vars) != 0 || len(env.Insertions) != 1 {
				t.Fatalf("directive parsed as an environment variable: %+v", env)
			}
			semantic := mapping.Resolve(workflowlang.EnvMappingView(env))
			if got := execution.CheckoutEnvironmentUnknown(semantic, runnerlang.MacOrLinux); got != tc.checkout {
				t.Errorf("checkout environment unknown: %v, want %v (complete=%v entries=%v)", got, tc.checkout, semantic.Complete(), mappingSummary(semantic))
			}
			if got := execution.ShellEnvironmentUnknown(semantic); got != tc.shell {
				t.Errorf("shell environment unknown: %v, want %v (complete=%v entries=%v)", got, tc.shell, semantic.Complete(), mappingSummary(semantic))
			}
			if got := execution.ShellPathUnknown(semantic); got != tc.path {
				t.Errorf("shell path unknown: %v, want %v (complete=%v entries=%v)", got, tc.path, semantic.Complete(), mappingSummary(semantic))
			}
		})
	}
}

func mappingSummary(resolved mapping.Mapping) []string {
	var summary []string
	for name, item := range resolved.Entries() {
		text, known := item.ScalarString()
		summary = append(summary, fmt.Sprintf("%s=%q (known=%v state=%d kind=%d)", name, text, known, item.State(), item.Kind()))
	}
	return summary
}
