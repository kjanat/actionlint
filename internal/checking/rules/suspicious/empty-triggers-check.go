package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// EmptyTriggers reports an "on" section that declares no events.
func EmptyTriggers(w declaration.WorkflowView) []reporting.Finding {
	pos, ok := w.OnPos()
	if !ok || !w.OnPresent() {
		return nil
	}
	for range w.On() {
		return nil
	}
	return []reporting.Finding{reporting.Warning("empty-triggers", "", &pos, "workflow declares no triggers in \"on\", so no event ever starts it")}
}
