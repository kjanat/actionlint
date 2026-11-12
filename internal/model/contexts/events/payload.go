// Package events adapts workflow trigger declarations to webhook payloads.
package events

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func Type(workflow workflowdecl.WorkflowView) exprtypes.ObjectType {
	if !workflow.Present() {
		return exprtypes.NewEmptyObjectType()
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
