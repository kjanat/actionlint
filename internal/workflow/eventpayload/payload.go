// Package eventpayload adapts workflow trigger declarations to webhook payloads.
package eventpayload

import (
	expr "actionlint.kjanat.dev/internal/expression"
	workflow "actionlint.kjanat.dev/internal/workflow"
)

func Type(w *workflow.Workflow) *expr.ObjectType {
	if w == nil {
		return expr.NewEmptyObjectType()
	}
	triggers := make([]Trigger, 0, len(w.On))
	for _, event := range w.On {
		trigger := Trigger{Name: event.EventName()}
		if hook, ok := event.(*workflow.WebhookEvent); ok {
			for _, action := range hook.Types {
				trigger.Actions = append(trigger.Actions, action.Value)
			}
		}
		triggers = append(triggers, trigger)
	}
	result := FromTriggers(triggers)
	// Retain the repository metadata covered by the existing workflow_run
	// injection regressions. The upstream head_repository projection omits homepage.
	if run, ok := result.Props["workflow_run"].(*expr.ObjectType); ok {
		if repo, ok := run.Props["head_repository"].(*expr.ObjectType); ok {
			repo.Props["homepage"] = expr.StringType{}
		}
	}
	return result
}
