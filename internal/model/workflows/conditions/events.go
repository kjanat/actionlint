package conditions

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// ReachableEvents excludes events only when the condition is provably false
// across execution statuses.
func ReachableEvents(events []string, condition string) []string {
	if condition == "" {
		return slices.Clone(events)
	}
	expression, err, _ := syntax.ParseConditionExpression(condition)
	if err != nil || expression == nil {
		return slices.Clone(events)
	}
	reachable := make([]string, 0, len(events))
	for _, event := range events {
		lookup := func(name string) (any, bool) {
			if strings.EqualFold(name, "github.event_name") {
				return event, true
			}
			return nil, false
		}
		if !NeverRunsWithVariables(expression, lookup) {
			reachable = append(reachable, event)
		}
	}
	return reachable
}
