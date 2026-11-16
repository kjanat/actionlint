// Package events adapts workflow trigger declarations to webhook payloads.
package events

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func Type(workflow workflows.Workflow) exprtypes.ObjectType {
	if !workflow.Present() {
		return exprtypes.NewEmptyObjectType()
	}
	var triggers []Trigger
	for _, event := range workflow.On {
		trigger := Trigger{Name: event.EventName()}
		var types []*definition.String
		switch hook := event.(type) {
		case *definition.WebhookEvent:
			types = hook.Types
		case *definition.RepositoryDispatchEvent:
			types = hook.Types
		}
		for _, action := range types {
			trigger.Actions = append(trigger.Actions, action.Text())
		}
		triggers = append(triggers, trigger)
	}
	return FromTriggers(triggers)
}
