// Package events adapts workflow trigger declarations to webhook payloads.
package events

import (
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func Type(workflow workflowdecl.Workflow) exprtypes.ObjectType {
	if !workflow.Present() {
		return exprtypes.NewEmptyObjectType()
	}
	var triggers []Trigger
	for _, event := range workflow.On {
		trigger := Trigger{Name: event.EventName()}
		var types []*workflowlang.String
		switch hook := event.(type) {
		case *workflowlang.WebhookEvent:
			types = hook.Types
		case *workflowlang.RepositoryDispatchEvent:
			types = hook.Types
		}
		for _, action := range types {
			trigger.Actions = append(trigger.Actions, action.Text())
		}
		triggers = append(triggers, trigger)
	}
	return FromTriggers(triggers)
}
