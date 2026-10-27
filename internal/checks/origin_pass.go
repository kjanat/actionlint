package checks

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
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

func (p *originPass) VisitJobPre(job declaration.JobView) error {
	origin := job.Origin()
	p.job = origin.Job
	return VisitWithOrigin(p.Pass, findingOrigin(origin), p.ignores, func() error { return p.Pass.VisitJobPre(job) })
}

func (p *originPass) VisitJobPost(job declaration.JobView) error {
	return VisitWithOrigin(p.Pass, findingOrigin(job.Origin()), p.ignores, func() error { return p.Pass.VisitJobPost(job) })
}

func (p *originPass) VisitStep(step declaration.StepView) error {
	return VisitWithOrigin(p.Pass, findingOrigin(step.Origin(p.job)), p.ignores, func() error { return p.Pass.VisitStep(step) })
}

func findingOrigin(owned declaration.Origin) *findingmodel.IgnoreOrigin {
	origin := &findingmodel.IgnoreOrigin{Job: owned.Job, StepID: owned.StepID, StepName: owned.StepName, Uses: owned.Uses}
	if owned.HasPosition {
		origin.StepPosition = new(owned.Position)
	}
	return origin
}
