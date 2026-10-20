// Package webhook describes valid Actions triggers independently of webhook payload observations.
package webhook

import (
	"slices"
	"sort"
)

// DefaultActivities returns activities selected when a workflow omits types.
// A known event with no activity types returns an empty slice. A nil slice for
// repository_dispatch represents unrestricted user-defined activities.
func DefaultActivities(event string) ([]string, bool) {
	activities, ok := AllWebhookTypes[event]
	if defaults, exists := defaultActivities[event]; exists {
		activities = defaults
	}
	return slices.Clone(activities), ok
}

// Fields returns the permitted configuration keys from the strict workflow
// schema, after documented Actions corrections.
func Fields(event string) []string { return slices.Clone(triggerFields[event]) }

// EventsWithField returns event names accepting a configuration key.
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
