package builtin

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestRuleRequireJobTimeout(t *testing.T) {
	jobPos := &workflowlang.Pos{Line: 4, Col: 3}
	valuePos := &workflowlang.Pos{Line: 6, Col: 22}
	missing := ":4:3: \"timeout-minutes\" is not set in job \"test\". it is required because \"require-job-timeout\" is enabled in the \"policy\" configuration. a job which does not set it is cancelled after the default of 360 minutes [require-job-timeout]"

	newJob := func(timeout *workflowlang.Float) *workflowlang.Job {
		return &workflowlang.Job{
			ID:             &workflowlang.String{Value: "test", Pos: jobPos},
			Steps:          []*workflowlang.Step{{Pos: &workflowlang.Pos{Line: 7, Col: 7}}},
			TimeoutMinutes: timeout,
			Pos:            jobPos,
		}
	}
	minutes := func(v float64) *workflowlang.Float {
		return &workflowlang.Float{Value: v, Pos: valuePos}
	}

	tests := []struct {
		what   string
		policy *repoconfig.JobTimeoutPolicy
		job    *workflowlang.Job
		want   []string
	}{
		{
			what:   "no policy",
			policy: nil,
			job:    newJob(nil),
		},
		{
			what:   "policy is disabled",
			policy: &repoconfig.JobTimeoutPolicy{},
			job:    newJob(nil),
		},
		{
			what:   "timeout is missing",
			policy: repoconfig.RequireJobTimeout(0),
			job:    newJob(nil),
			want:   []string{missing},
		},
		{
			what:   "timeout is set",
			policy: repoconfig.RequireJobTimeout(0),
			job:    newJob(minutes(45)),
		},
		{
			what:   "no maximum is configured",
			policy: repoconfig.RequireJobTimeout(0),
			job:    newJob(minutes(4320)),
		},
		{
			what:   "timeout is below the maximum",
			policy: repoconfig.RequireJobTimeout(30),
			job:    newJob(minutes(15)),
		},
		{
			what:   "timeout is equal to the maximum",
			policy: repoconfig.RequireJobTimeout(30),
			job:    newJob(minutes(30)),
		},
		{
			what:   "timeout is above the maximum",
			policy: repoconfig.RequireJobTimeout(30),
			job:    newJob(minutes(45)),
			want: []string{
				":6:22: \"timeout-minutes\" is 45 in job \"test\". it must not be larger than 30 because \"max-minutes\" of \"require-job-timeout\" is set in the \"policy\" configuration [require-job-timeout]",
			},
		},
		{
			what:   "maximum is a fraction",
			policy: repoconfig.RequireJobTimeout(30.5),
			job:    newJob(minutes(45)),
			want: []string{
				":6:22: \"timeout-minutes\" is 45 in job \"test\". it must not be larger than 30.5 because \"max-minutes\" of \"require-job-timeout\" is set in the \"policy\" configuration [require-job-timeout]",
			},
		},
		{
			what:   "timeout is missing while a maximum is configured",
			policy: repoconfig.RequireJobTimeout(30),
			job:    newJob(nil),
			want:   []string{missing},
		},
		{
			what:   "timeout is an expression",
			policy: repoconfig.RequireJobTimeout(30),
			job: newJob(&workflowlang.Float{
				Value:      45,
				Expression: &workflowlang.String{Value: "matrix.timeout", Pos: valuePos},
				Pos:        valuePos,
			}),
		},
		{
			what:   "job calls a reusable workflow",
			policy: repoconfig.RequireJobTimeout(30),
			job: &workflowlang.Job{
				ID: &workflowlang.String{Value: "test", Pos: jobPos},
				WorkflowCall: &workflowlang.WorkflowCall{
					Uses: &workflowlang.String{Value: "./.github/workflows/ci.yaml", Pos: &workflowlang.Pos{Line: 5, Col: 11}},
				},
				Steps: []*workflowlang.Step{{Pos: &workflowlang.Pos{Line: 7, Col: 7}}},
				Pos:   jobPos,
			},
		},
		{
			what:   "job has no steps",
			policy: repoconfig.RequireJobTimeout(30),
			job: &workflowlang.Job{
				ID:  &workflowlang.String{Value: "test", Pos: jobPos},
				Pos: jobPos,
			},
		},
		{
			what:   "job has an empty steps section",
			policy: repoconfig.RequireJobTimeout(30),
			job: &workflowlang.Job{
				ID:    &workflowlang.String{Value: "test", Pos: jobPos},
				Steps: []*workflowlang.Step{},
				Pos:   jobPos,
			},
			want: []string{missing},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			r := NewRuleRequireJobTimeout(tc.policy)
			if err := r.VisitJobPre(ownedJobFixture(tc.job)); err != nil {
				t.Fatal(err)
			}
			errs := r.Errs()
			if len(errs) != len(tc.want) {
				t.Fatalf("wanted %d errors but have %d: %v", len(tc.want), len(errs), errs)
			}
			for i, want := range tc.want {
				if have := errs[i].String(); have != want {
					t.Fatalf("wanted error %q but have %q", want, have)
				}
			}
		})
	}
}

func TestRuleRequireJobTimeoutRange(t *testing.T) {
	for _, tc := range []struct {
		name, kind      string
		min, max, value float64
		expression      bool
		call            bool
	}{
		{name: "below minimum", min: 5, max: 30, value: 4, kind: "min-minutes"},
		{name: "at minimum", min: 5, max: 30, value: 5},
		{name: "within range", min: 5, max: 30, value: 15},
		{name: "at maximum", min: 5, max: 30, value: 30},
		{name: "above maximum", min: 5, max: 30, value: 31, kind: "max-minutes"},
		{name: "minimum only", min: 5, value: 4, kind: "min-minutes"},
		{name: "fractional minimum", min: 1.5, value: 1.25, kind: "min-minutes"},
		{name: "equal bounds", min: 5, max: 5, value: 5},
		{name: "expression", min: 5, max: 30, value: 4, expression: true},
		{name: "reusable workflow", min: 5, value: 4, call: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := repoconfig.RequireJobTimeoutRange(tc.min, tc.max)
			if err != nil {
				t.Fatal(err)
			}
			pos := &workflowlang.Pos{Line: 6, Col: 22}
			job := &workflowlang.Job{ID: &workflowlang.String{Value: "test"}, Pos: &workflowlang.Pos{Line: 4, Col: 3}, Steps: []*workflowlang.Step{}, TimeoutMinutes: &workflowlang.Float{Value: tc.value, Pos: pos}}
			if tc.expression {
				job.TimeoutMinutes.Expression = &workflowlang.String{Value: "matrix.timeout", Pos: pos}
			}
			if tc.call {
				job.WorkflowCall = &workflowlang.WorkflowCall{}
			}
			rule := NewRuleRequireJobTimeout(policy)
			if err := rule.VisitJobPre(ownedJobFixture(job)); err != nil {
				t.Fatal(err)
			}
			errs := rule.Errs()
			if tc.kind == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) != 1 || errs[0].Start.Line != pos.Line || errs[0].Start.Column != pos.Col || !strings.Contains(errs[0].Message, tc.kind) {
				t.Fatalf("expected %s error at %v, got %v", tc.kind, pos, errs)
			}
		})
	}
}
