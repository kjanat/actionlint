package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyRun reports a run step whose script is empty.
func EmptyRun(step declaration.StepView) []reporting.Finding {
	run, ok := step.Exec().Run()
	// Only a string run value carries a source scalar; other kinds already
	// have a syntax-check finding.
	if !ok || !run.Run().SourceScalar().HasSource || run.Run().Value() != "" {
		return nil
	}
	finding := reporting.Warning("empty-run", "", reporting.ViewPosition(run.RunPos()), "step runs an empty script, so it does nothing")
	finding.Step = step
	return []reporting.Finding{finding}
}
