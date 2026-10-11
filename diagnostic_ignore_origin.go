package actionlint

// diagnosticIgnoreOrigin distinguishes executions of shared YAML anchors.
// It never changes displayed locations or the scope of inline comments.
type diagnosticIgnoreOrigin struct {
	job, stepID, stepName, uses string
}

func newDiagnosticIgnoreOrigin(job string, step *Step) *diagnosticIgnoreOrigin {
	origin := &diagnosticIgnoreOrigin{job: job}
	value := func(s *String) string {
		if s == nil {
			return ""
		}
		if literal := literalExpressionValue(s.Value); literal != nil {
			return *literal
		}
		return s.Value
	}
	if step != nil {
		origin.stepID, origin.stepName = value(step.ID), value(step.Name)
		if action, ok := step.Exec.(*ExecAction); ok {
			origin.uses = value(action.Uses)
		}
	}
	return origin
}

func (origin *diagnosticIgnoreOrigin) matches(job, step, uses string) bool {
	return (job == "" || job == origin.job) && (step == "" || step == origin.stepID || step == origin.stepName) && (uses == "" || uses == origin.uses)
}

func jobDiagnosticIgnoreOrigin(job *Job) *diagnosticIgnoreOrigin {
	origin := newDiagnosticIgnoreOrigin("", nil)
	if job.ID != nil {
		origin.job = job.ID.Value
	}
	if job.WorkflowCall != nil && job.WorkflowCall.Uses != nil {
		origin.uses = job.WorkflowCall.Uses.Value
		if literal := literalExpressionValue(origin.uses); literal != nil {
			origin.uses = *literal
		}
	}
	return origin
}

// Asynchronous analyzers snapshot the origin before scheduling their work.
// Synchronous passes can safely annotate only the findings produced by this visit.
func visitWithIgnoreOrigin(pass Pass, origin *diagnosticIgnoreOrigin, visit func() error) error {
	rule, ok := pass.(Rule)
	if !ok || rule.Config() == nil || len(rule.Config().Ignores) == 0 {
		return visit()
	}
	switch rule := pass.(type) {
	case *RuleShellcheck:
		rule.ignoreOrigin = origin
		return visit()
	case *ruffRule:
		rule.ignoreOrigin = origin
		return visit()
	}
	start := len(rule.Errs())
	err := visit()
	for _, finding := range rule.Errs()[start:] {
		if finding.source == nil && finding.ignoreOrigin == nil {
			finding.ignoreOrigin = origin
		}
	}
	return err
}
