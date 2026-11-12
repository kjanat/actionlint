package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// NumericValues reports a max-parallel GitHub truncates or that cannot take effect.
func NumericValues(job declaration.JobView) []reporting.Finding {
	value := job.Strategy().MaxParallel()
	if !value.Present() || value.Expression().Present() {
		return nil
	}
	pos := reporting.ViewPosition(value.Pos())
	var out []reporting.Finding
	if from := value.TruncatedFrom(); from != "" {
		out = append(out, reporting.Warning("numeric-values", "truncated", pos, "\"max-parallel\" value %s is truncated to %d", from, value.Value()))
	}
	if value.Value() <= 0 {
		out = append(out, reporting.Warning("numeric-values", "non-positive", pos, "\"max-parallel\" must be greater than zero but is %d", value.Value()))
	}
	return out
}
