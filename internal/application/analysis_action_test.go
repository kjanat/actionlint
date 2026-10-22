package application

import (
	"testing"

	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

const standaloneComposite = "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"

func TestStandaloneActionCustomRules(t *testing.T) {
	for _, name := range []string{"step-name", "action", "shellcheck", "ruff", "deprecated-commands", "executable-bit"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			custom := workflowvisit.NewBase(name, "custom rule")
			request := analysisRequest{
				WorkingDir: t.TempDir(),
				Sources: []sourceUnit{
					{Path: "action.yml", Content: []byte(standaloneComposite)},
					{Path: "ci.yml", Content: []byte(commandGoodWorkflow)},
				},
				OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
					calls++
					return []workflowvisit.Rule{&custom}
				},
			}
			result, err := runTestAnalysis(t.Context(), request)
			if err != nil || len(result.Diagnostics) != 0 || calls != 1 {
				t.Fatalf("expected hook once for workflow only; calls=%d result=%+v error=%v", calls, result, err)
			}
		})
	}
}
