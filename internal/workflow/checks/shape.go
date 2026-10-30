package checks

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// The shape checks report declarations GitHub accepts that cannot do what
// their author wrote. They warn rather than fail, because the service runs them.

func warning(rule, code string, p *workflow.Pos, format string, args ...any) Finding {
	return Finding{Rule: rule, Code: code, Severity: "warning", Message: fmt.Sprintf(format, args...), Start: position(p)}
}

// EmptyTriggers reports an "on" section that declares no events.
func EmptyTriggers(w declaration.WorkflowView) []Finding {
	pos, ok := w.OnPos()
	if !ok || !w.OnPresent() {
		return nil
	}
	for range w.On() {
		return nil
	}
	return []Finding{warning("empty-triggers", "", &pos, "workflow declares no triggers in \"on\", so no event ever starts it")}
}

// EmptyJob reports a job that runs no steps. Reusable workflow calls have none.
func EmptyJob(job declaration.JobView) []Finding {
	if job.WorkflowCall().Present() || job.DeclaredUses().Present() {
		return nil
	}
	for range job.Steps() {
		return nil
	}
	return []Finding{warning("empty-job", "", viewPosition(job.Pos()), "job %q has no steps, so it does nothing", job.ID().Value())}
}

// EmptyRun reports a run step whose script is empty.
func EmptyRun(step declaration.StepView) []Finding {
	run, ok := step.Exec().Run()
	// Only a string run value carries a source scalar; other kinds already
	// have a syntax-check finding.
	if !ok || !run.Run().SourceScalar().HasSource || run.Run().Value() != "" {
		return nil
	}
	finding := warning("empty-run", "", viewPosition(run.RunPos()), "step runs an empty script, so it does nothing")
	finding.Step = step
	return []Finding{finding}
}

// NumericValues reports a max-parallel GitHub truncates or that cannot take effect.
func NumericValues(job declaration.JobView) []Finding {
	value := job.Strategy().MaxParallel()
	if !value.Present() || value.Expression().Present() {
		return nil
	}
	pos := viewPosition(value.Pos())
	var out []Finding
	if from := value.TruncatedFrom(); from != "" {
		out = append(out, warning("numeric-values", "truncated", pos, "\"max-parallel\" value %s is truncated to %d", from, value.Value()))
	}
	if value.Value() <= 0 {
		out = append(out, warning("numeric-values", "non-positive", pos, "\"max-parallel\" must be greater than zero but is %d", value.Value()))
	}
	return out
}
