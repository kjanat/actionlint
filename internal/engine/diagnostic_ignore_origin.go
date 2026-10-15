package engine

import (
	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// diagnosticIgnoreOrigin distinguishes executions of shared YAML anchors.
// It never changes displayed locations or the scope of inline comments.
type diagnosticIgnoreOrigin struct {
	job, stepID, stepName, uses string
	// metadata uses the owning action's YAML location for inner selectors.
	metadata bool
}

func newDiagnosticIgnoreOrigin(job string, step *workflowlang.Step) *diagnosticIgnoreOrigin {
	origin := &diagnosticIgnoreOrigin{job: job}
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
		origin.stepID, origin.stepName = value(step.ID), value(step.Name)
		if action, ok := step.Exec.(*workflowlang.ExecAction); ok {
			origin.uses = value(action.Uses)
		}
	}
	return origin
}

func (origin *diagnosticIgnoreOrigin) matches(job, step, uses string) bool {
	return (job == "" || job == origin.job) && (step == "" || step == origin.stepID || step == origin.stepName) && (uses == "" || uses == origin.uses)
}

func jobDiagnosticIgnoreOrigin(job *workflowlang.Job) *diagnosticIgnoreOrigin {
	origin := newDiagnosticIgnoreOrigin("", nil)
	if job.ID != nil {
		origin.job = job.ID.Value
	}
	if job.WorkflowCall != nil && job.WorkflowCall.Uses != nil {
		origin.uses = job.WorkflowCall.Uses.Value
		if literal := exprlang.LiteralExpressionValue(origin.uses); literal != nil {
			origin.uses = *literal
		}
	}
	return origin
}

// Asynchronous analyzers snapshot the origin before scheduling their work.
// Synchronous passes can safely annotate only the findings produced by this visit.
func visitWithIgnoreOrigin(pass Pass, origin *diagnosticIgnoreOrigin, visit func() error) error {
	rule, ok := pass.(Rule)
	if !ok {
		return visit()
	}
	// Metadata can have file-specific ignores even when its caller has none.
	_, actionRule := rule.(metadataParticipant)
	hasIgnores := ruleConfig(rule) != nil && len(ruleConfig(rule).Ignores) != 0
	if !actionRule && !hasIgnores {
		return visit()
	}
	if participant, ok := pass.(originParticipant); ok {
		participant.setIgnoreOrigin(origin)
		return visit()
	}

	start := len(rule.Errs())
	err := visit()
	for _, finding := range rule.Errs()[start:] {
		if !hasIgnores && (finding.metadataFinding == nil || finding.source == nil) {
			continue
		}
		if finding.source == nil && finding.ignoreOrigin == nil {
			finding.ignoreOrigin = origin
		} else if finding.source != nil && finding.ignoreOrigin != nil {
			// Each invocation owns its origin; shared metadata never stores caller state.
			inner := *finding.ignoreOrigin
			inner.job = origin.job
			finding.ignoreOrigin = &inner
		}
	}
	return err
}

// originParticipant snapshots context for asynchronous diagnostic producers.
type originParticipant interface{ setIgnoreOrigin(*diagnosticIgnoreOrigin) }
