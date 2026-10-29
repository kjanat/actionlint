package declaration

import (
	"slices"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestOriginOwnsSelectorValuesAndPosition(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"plain", "selector", "selector"},
		{"literal", "${{ 'selector' }}", "selector"},
		{"dynamic", "${{ inputs.selector }}", "${{ inputs.selector }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := &workflowlang.String{Value: tc.value}
			step := &workflowlang.Step{Pos: &workflowlang.Pos{Line: 3, Col: 7}, ID: value, Name: value, Exec: &workflowlang.ExecAction{Uses: value}}
			bound := Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"build": {Steps: []*workflowlang.Step{step}}}}, false, "").Bind("origin-fixture")
			job, present := bound.Workflow().Job("build")
			if !present {
				t.Fatal("owned job unavailable")
			}
			view := slices.Collect(job.Steps())[0]
			value.Value, step.Pos.Line = "changed", 999
			want := Origin{Job: "Build", StepID: tc.want, StepName: tc.want, Uses: tc.want, Position: workflowlang.Pos{Line: 3, Col: 7}, HasPosition: true}
			origin := view.Origin("Build")
			if origin != want {
				t.Fatalf("owned origin = %+v; want %+v", origin, want)
			}
			origin.Position.Line = 999
			if fresh := view.Origin("Build"); fresh != want {
				t.Fatalf("returned position changed stored origin: %+v", fresh)
			}
		})
	}
	if got := (StepView{}).Origin("Build"); got != (Origin{Job: "Build"}) {
		t.Fatalf("missing step origin = %+v", got)
	}
}

func TestJobOriginOwnsUsesSelector(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"plain", "org/repo/.github/workflows/check.yml@main", "org/repo/.github/workflows/check.yml@main"},
		{"literal", "${{ './.github/workflows/check.yml' }}", "./.github/workflows/check.yml"},
		{"dynamic", "${{ inputs.workflow }}", "${{ inputs.workflow }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uses := &workflowlang.String{Value: tc.value}
			job := &workflowlang.Job{ID: &workflowlang.String{Value: "Build"}, WorkflowCall: &workflowlang.WorkflowCall{Uses: uses}}
			bound := Publish("", &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"build": job}}, false, "").Bind("origin-fixture")
			view, present := bound.Workflow().Job("build")
			if !present {
				t.Fatal("owned job unavailable")
			}
			job.ID.Value, uses.Value = "changed", "changed"
			want := Origin{Job: "Build", Uses: tc.want}
			if got := view.Origin(); got != want {
				t.Fatalf("job origin = %+v; want %+v", got, want)
			}
		})
	}
	if got := (JobView{}).Origin(); got != (Origin{}) {
		t.Fatalf("missing job origin = %+v", got)
	}
}
