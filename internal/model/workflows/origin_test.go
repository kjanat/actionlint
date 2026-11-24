package workflows

import (
	"reflect"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestIgnoreOriginReadsSelectorValuesAndPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, value, want string }{
		{"plain", "selector", "selector"},
		{"literal", "${{ 'selector' }}", "selector"},
		{"dynamic", "${{ inputs.selector }}", "${{ inputs.selector }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value := &definition.String{Value: tc.value}
			step := &definition.Step{Pos: &sourcetext.Position{Line: 3, Column: 7}, ID: value, Name: value, Exec: &definition.ExecAction{Uses: value}}
			bound := Bind("origin-fixture", "", &definition.Workflow{Jobs: map[string]*definition.Job{"build": {Steps: []*definition.Step{step}}}}, false, "")
			job, present := bound.Workflow().Job("build")
			if !present {
				t.Fatal("job build unavailable")
			}
			view := slices.Collect(job.Steps())[0]
			want := IgnoreOrigin{Job: "Build", StepID: tc.want, StepName: tc.want, Uses: tc.want, StepPosition: &sourcetext.Position{Line: 3, Column: 7}}
			origin := view.IgnoreOrigin("Build")
			if !reflect.DeepEqual(*origin, want) {
				t.Fatalf("origin = %+v; want %+v", origin, want)
			}
			origin.StepPosition.Line = 999
			if fresh := view.IgnoreOrigin("Build"); !reflect.DeepEqual(*fresh, want) {
				t.Fatalf("returned position changed stored origin: %+v", fresh)
			}
		})
	}
	if got := (BoundStep{}).IgnoreOrigin("Build"); *got != (IgnoreOrigin{Job: "Build"}) {
		t.Fatalf("missing step origin = %+v", got)
	}
}

func TestJobIgnoreOriginReadsUsesSelector(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, value, want string }{
		{"plain", "org/repo/.github/workflows/check.yml@main", "org/repo/.github/workflows/check.yml@main"},
		{"literal", "${{ './.github/workflows/check.yml' }}", "./.github/workflows/check.yml"},
		{"dynamic", "${{ inputs.workflow }}", "${{ inputs.workflow }}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			uses := &definition.String{Value: tc.value}
			job := &definition.Job{ID: &definition.String{Value: "Build"}, WorkflowCall: &definition.WorkflowCall{Uses: uses}}
			bound := Bind("origin-fixture", "", &definition.Workflow{Jobs: map[string]*definition.Job{"build": job}}, false, "")
			view, present := bound.Workflow().Job("build")
			if !present {
				t.Fatal("job build unavailable")
			}
			want := IgnoreOrigin{Job: "Build", Uses: tc.want}
			if got := view.IgnoreOrigin(); *got != want {
				t.Fatalf("job origin = %+v; want %+v", got, want)
			}
		})
	}
	if got := (BoundJob{}).IgnoreOrigin(); *got != (IgnoreOrigin{}) {
		t.Fatalf("missing job origin = %+v", got)
	}
}
