package conventions

import (
	"strings"
	"testing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestMissingActionMetadataUsesIndependentStepInputs(t *testing.T) {
	check := MissingActionMetadataCheck(nil)
	for _, uses := range []string{"./local", "$/mapped", "docker://alpine", "${{ inputs.action }}", "invalid", "actions/checkout@v4"} {
		t.Run(uses, func(t *testing.T) {
			step := &workflowlang.Step{Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: uses, Pos: &workflowlang.Pos{Line: 7, Col: 9}}}}
			snapshot := workflowdecl.Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"test": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("metadata-step").Workflow()
			job, _ := snapshot.Job("test")
			var checkedStep workflowdecl.StepView
			for step := range job.Steps() {
				checkedStep = step
				break
			}
			for range 2 {
				findings := check.Step(checkedStep)
				if uses != "actions/checkout@v4" {
					if len(findings) != 0 {
						t.Fatalf("non-repository reference was checked: %+v", findings)
					}
					continue
				}
				if len(findings) != 1 || !strings.Contains(findings[0].Message, "lookup-disabled") || findings[0].Start.Line != 7 || findings[0].Start.Column != 9 || findings[0].Step.Reference() != checkedStep.Reference() {
					t.Fatalf("missing metadata finding lost its outcome or source: %+v", findings)
				}
			}
		})
	}
}
