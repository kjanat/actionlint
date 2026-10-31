// Package eventpayload adapts workflow trigger declarations to webhook payloads.
package eventpayload

import (
	expr "actionlint.kjanat.dev/v2/internal/expression"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

func Type(workflow workflowdecl.WorkflowView) expr.ObjectType {
	if !workflow.Present() {
		return expr.NewEmptyObjectType()
	}
	var triggers []Trigger
	for event := range workflow.On() {
		trigger := Trigger{Name: event.EventName()}
		if hook, ok := event.WebhookEvent(); ok {
			for action := range hook.Types() {
				trigger.Actions = append(trigger.Actions, action.Value())
			}
		} else if hook, ok := event.RepositoryDispatchEvent(); ok {
			for action := range hook.Types() {
				trigger.Actions = append(trigger.Actions, action.Value())
			}
		}
		triggers = append(triggers, trigger)
	}
	return FromTriggers(triggers)
}
