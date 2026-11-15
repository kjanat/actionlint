package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
)

// RunnerLabelRule is the single definition of the runner-label rule.
var RunnerLabelRule = &define.Rule{Name: "runner-label", Description: "Report unknown or incompatible runner labels", Category: "correctness", Subcategory: "runners", Recommended: true, Help: "Correct the runner label or declare repository-specific labels in repository.runners.",
	Checks: func(c define.Inputs) define.Checks {
		labels := runnerlang.LabelSet{}
		if known, available := configuration.RepositoryFacts(c.Config, c.Repository).RunnerLabels.Value(); available {
			labels = runnerlang.KnownLabels(known.Values())
		}
		return RunnerLabelChecks(c.Runners, labels)
	},
}
