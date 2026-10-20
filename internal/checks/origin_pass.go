package checks

import (
	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// WithOrigins annotates results while a synchronous pass still has its owner.
func WithOrigins(pass workflowvisit.Pass, config *repoconfig.Config) workflowvisit.Pass {
	return &originPass{Pass: pass, ignores: HasConfiguredIgnores(config)}
}

type originPass struct {
	workflowvisit.Pass
	job     string
	ignores bool
}

func (p *originPass) VisitJobPre(job *workflowlang.Job) error {
	p.job = ""
	if job.ID != nil {
		p.job = job.ID.Value
	}
	return VisitWithOrigin(p.Pass, findingmodel.JobOrigin(job), p.ignores, func() error { return p.Pass.VisitJobPre(job) })
}

func (p *originPass) VisitJobPost(job *workflowlang.Job) error {
	return VisitWithOrigin(p.Pass, findingmodel.JobOrigin(job), p.ignores, func() error { return p.Pass.VisitJobPost(job) })
}

func (p *originPass) VisitStep(step *workflowlang.Step) error {
	return VisitWithOrigin(p.Pass, findingmodel.StepOrigin(p.job, step), p.ignores, func() error { return p.Pass.VisitStep(step) })
}
