package events

import (
	"slices"
	"sort"
)

// DefaultActivities returns an empty slice for a known event with no activity
// types. A nil slice for repository_dispatch represents unrestricted user-defined activities.
func DefaultActivities(event string) ([]string, bool) {
	activities, ok := AllWebhookTypes[event]
	if defaults, exists := defaultActivities[event]; exists {
		activities = defaults
	}
	return slices.Clone(activities), ok
}

func TriggerFields(event string) []string { return slices.Clone(triggerFields[event]) }

func EventsWithField(field string) []string {
	var events []string
	for event, fields := range triggerFields {
		if slices.Contains(fields, field) {
			events = append(events, event)
		}
	}
	sort.Strings(events)
	return events
}
