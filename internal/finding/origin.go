package finding

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

type Owner struct {
	Job  string
	Step *workflowlang.Pos
}

// IgnoreOrigin distinguishes executions of shared YAML anchors.
// It never changes displayed locations or the scope of inline comments.
type IgnoreOrigin struct {
	StepPosition                *workflowlang.Pos
	Job, StepID, StepName, Uses string
	// metadata uses the owning action's YAML location for inner selectors.
	Metadata bool
}

func StepOrigin(job string, step *workflowlang.Step) *IgnoreOrigin {
	origin := &IgnoreOrigin{Job: job}
	value := func(s *workflowlang.String) string {
		if s == nil {
			return ""
		}
		if literal := exprlang.LiteralExpressionValue(s.Value); literal != nil {
			return *literal
		}
		return s.Value
	}
	if step != nil {
		origin.StepPosition = step.Pos
		origin.StepID, origin.StepName = value(step.ID), value(step.Name)
		if action, ok := step.Exec.(*workflowlang.ExecAction); ok {
			origin.Uses = value(action.Uses)
		}
	}
	return origin
}

func (origin *IgnoreOrigin) Matches(job, step, uses string) bool {
	return (job == "" || job == origin.Job) && (step == "" || step == origin.StepID || step == origin.StepName) && (uses == "" || uses == origin.Uses)
}

func JobOrigin(job *workflowlang.Job) *IgnoreOrigin {
	origin := StepOrigin("", nil)
	if job.ID != nil {
		origin.Job = job.ID.Value
	}
	if job.WorkflowCall != nil && job.WorkflowCall.Uses != nil {
		origin.Uses = job.WorkflowCall.Uses.Value
		if literal := exprlang.LiteralExpressionValue(origin.Uses); literal != nil {
			origin.Uses = *literal
		}
	}
	return origin
}

type SuppressionOrigin struct {
	Path        string
	First, Last int
}
