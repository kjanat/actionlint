package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyTriggers reports an "on" section that declares no events.
func EmptyTriggers(w declaration.Workflow) []reporting.StepFinding {
	if !w.Present() || w.OnPos == nil || w.On == nil || len(w.On) > 0 {
		return nil
	}
	return []reporting.StepFinding{reporting.Warning("empty-triggers", "", w.OnPos, "workflow declares no triggers in \"on\", so no event ever starts it")}
}
