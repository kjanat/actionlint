// Package eventpayload adapts workflow trigger declarations to webhook payloads.
package eventpayload

import (
	expr "actionlint.kjanat.dev/v2/internal/expression"
	workflow "actionlint.kjanat.dev/v2/internal/workflow"
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
	return FromTriggers(triggers)
}
