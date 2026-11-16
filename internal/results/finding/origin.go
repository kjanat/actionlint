package finding

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

type Owner struct {
	Job  string
	Step *sourcetext.Position
}

// IgnoreOrigin distinguishes executions of shared YAML anchors.
// It never changes displayed locations or the scope of inline comments.
type IgnoreOrigin struct {
	StepPosition                *sourcetext.Position
	Job, StepID, StepName, Uses string
	// metadata uses the owning action's YAML location for inner selectors.
	Metadata bool
}

func (origin *IgnoreOrigin) Matches(job, step, uses string) bool {
	return (job == "" || job == origin.Job) && (step == "" || step == origin.StepID || step == origin.StepName) && (uses == "" || uses == origin.Uses)
}
