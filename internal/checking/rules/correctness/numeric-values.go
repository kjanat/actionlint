package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var NumericValuesRule = &define.Rule{
	Name:        "numeric-values",
	Description: "Report numeric values GitHub truncates or that cannot take effect",
	Category:    "correctness",
	Subcategory: "values",
	Recommended: true,
	Help:        "Set max-parallel to a positive whole number.",
	Examples: []rule.Example{rule.WorkflowExample("",
		"    strategy:\n      max-parallel: 1.5\n      matrix:\n        n: [1, 2]\n", "    strategy:\n      max-parallel: 1\n      matrix:\n        n: [1, 2]\n")},
	Checks: define.Fixed(define.Checks{Job: NumericValues}),
}

// NumericValues reports a max-parallel GitHub truncates or that cannot take effect.
func NumericValues(job workflows.BoundJob) []reporting.StepFinding {
	if !job.Present() || job.Strategy == nil {
		return nil
	}
	value := job.Strategy.MaxParallel
	if value == nil || value.Expression != nil {
		return nil
	}
	pos := value.Pos
	var out []reporting.StepFinding
	if from := value.TruncatedFrom; from != "" {
		out = append(out, reporting.Warning("numeric-values", "truncated", pos, "\"max-parallel\" value %s is truncated to %d", from, value.Value))
	}
	if value.Value <= 0 {
		out = append(out, reporting.Warning("numeric-values", "non-positive", pos, "\"max-parallel\" must be greater than zero but is %d", value.Value))
	}
	return out
}
